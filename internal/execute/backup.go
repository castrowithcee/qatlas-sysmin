package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

const (
	// DefaultBackupRoot is where backups are kept when Executor.BackupRoot is empty.
	DefaultBackupRoot = "/var/lib/sysmin/backups"

	// DefaultBackupImage is the helper image of volume backups: busybox 1.37.0, pinned by the digest of its
	// multi-platform index.
	DefaultBackupImage = "busybox@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"

	backupDirEnv   = "SYSMIN_BACKUP_DIR"
	backupDirMode  = 0o700
	backupTimeForm = "20060102T150405Z"
)

var (
	// backupNameRE matches the name of a backup directory; the form sorts in time order.
	backupNameRE = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z$`)
	projectRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	// volumeRE is the set of Docker volume names; anything else could be read as a path or a mount option.
	volumeRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
)

func (e *Executor) backupRoot() string {
	if e.BackupRoot != "" {
		return e.BackupRoot
	}
	return DefaultBackupRoot
}

func (e *Executor) backupImage() string {
	if e.BackupImage != "" {
		return e.BackupImage
	}
	return DefaultBackupImage
}

func isBackupName(name string) bool {
	if !backupNameRE.MatchString(name) {
		return false
	}
	_, err := time.Parse(backupTimeForm, name)
	return err == nil
}

// projectBackupDir is the directory that holds the backups of one project.
func projectBackupDir(root, project string) (string, error) {
	if !projectRE.MatchString(project) {
		return "", fmt.Errorf("invalid project name %q", project)
	}
	return filepath.Join(root, project), nil
}

// createBackupDir makes the directory for a new backup. A name taken already moves the time stamp on by a
// second, so the name stays unique and sortable.
func createBackupDir(root, project string, now time.Time) (string, error) {
	pdir, err := projectBackupDir(root, project)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(pdir, backupDirMode); err != nil {
		return "", err
	}
	t := now.UTC().Truncate(time.Second)
	for range 3600 {
		dir := filepath.Join(pdir, t.Format(backupTimeForm))
		err := os.Mkdir(dir, backupDirMode)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		t = t.Add(time.Second)
	}
	return "", errors.New("no free backup directory name")
}

// removeBackupDir deletes one backup directory after checking that it is a real directory, not a link,
// directly below the project directory and named like a backup.
func removeBackupDir(root, project, dir string) error {
	pdir, err := projectBackupDir(root, project)
	if err != nil {
		return err
	}
	if filepath.Dir(dir) != pdir || !isBackupName(filepath.Base(dir)) {
		return fmt.Errorf("%s is not a backup directory of project %s", dir, project)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return os.RemoveAll(dir)
}

// retainBackups keeps the newest keep backup directories of the project, and the one just made, and removes
// older ones. Only plain directories with a backup name directly below the project directory count.
func retainBackups(root, project, current string, keep int) (removed []string, err error) {
	pdir, err := projectBackupDir(root, project)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(pdir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ent := range entries {
		if ent.Type().IsDir() && isBackupName(ent.Name()) {
			names = append(names, ent.Name())
		}
	}
	sort.Strings(names)
	if len(names) <= keep {
		return nil, nil
	}
	var errs []error
	for _, name := range names[:len(names)-keep] {
		dir := filepath.Join(pdir, name)
		if dir == current {
			continue
		}
		if err := removeBackupDir(root, project, dir); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, dir)
	}
	return removed, errors.Join(errs...)
}

func backupParams(st config.ResolvedStep) (*config.BackupParams, error) {
	b := st.Backup
	if b == nil {
		return nil, errors.New("backup step has no parameters")
	}
	if (b.Command == "") == (b.Volume == "") {
		return nil, errors.New("backup step needs exactly one of command and volume")
	}
	if b.Keep < 1 {
		return nil, errors.New("backup step keeps less than one backup")
	}
	if b.Command != "" {
		if err := needDir(st); err != nil {
			return nil, err
		}
		if strings.TrimSpace(b.Command) == "" {
			return nil, errors.New("backup step has no command line")
		}
	}
	return b, nil
}

// volumeBackupCommand archives a volume through a short-lived container without network, with the volume
// mounted read-only. No shell is involved.
func volumeBackupCommand(image, volume, dir string) (Command, error) {
	if !strings.Contains(image, "@sha256:") {
		return Command{}, fmt.Errorf("backup image %q is not pinned by digest", image)
	}
	if !volumeRE.MatchString(volume) {
		return Command{}, fmt.Errorf("volume name %q is not valid", volume)
	}
	if strings.ContainsAny(dir, ":,") {
		return Command{}, fmt.Errorf("backup directory %q cannot be mounted", dir)
	}
	return Command{Name: "docker", Args: []string{"run", "--rm", "--network", "none",
		"-v", volume + ":/source:ro", "-v", dir + ":/backup", image,
		"tar", "-C", "/source", "-czf", "/backup/" + volume + ".tar.gz", "."}}, nil
}

func runBackup(ctx context.Context, s *stepRun, st config.ResolvedStep) error {
	b, err := backupParams(st)
	if err != nil {
		return err
	}
	root := s.e.backupRoot()
	dir, err := createBackupDir(root, s.project, s.e.now())
	if err != nil {
		return err
	}
	s.backupDir = dir
	s.logf("backup directory %s", dir)
	s.note("backup directory: %s", dir)

	var c Command
	if b.Command != "" {
		c = Command{Name: shell, Args: []string{"-c", b.Command}, Dir: st.Dir, Env: []string{backupDirEnv + "=" + dir}}
	} else if c, err = volumeBackupCommand(s.e.backupImage(), b.Volume, dir); err != nil {
		_ = removeBackupDir(root, s.project, dir)
		return err
	}
	if _, err := s.run(ctx, c); err != nil {
		if rerr := removeBackupDir(root, s.project, dir); rerr != nil {
			s.note("could not remove the failed backup directory: %v", rerr)
		}
		return err
	}
	removed, err := retainBackups(root, s.project, dir, b.Keep)
	for _, r := range removed {
		s.note("removed old backup: %s", r)
	}
	if err != nil {
		// The backup itself succeeded; a leftover old backup must not stop the update.
		s.note("could not remove an old backup: %v", err)
	}
	return nil
}

func describeBackup(d describeCtx, st config.ResolvedStep) ([]string, error) {
	b, err := backupParams(st)
	if err != nil {
		return nil, err
	}
	pdir, err := projectBackupDir(d.backupRoot, d.project)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(pdir, "<timestamp>")
	var first string
	if b.Command != "" {
		c := Command{Name: shell, Args: []string{"-c", b.Command}}
		first = fmt.Sprintf("backup command in %s/: %s (in %s)", dir, displayCommand(c), st.Dir)
	} else {
		c, err := volumeBackupCommand(DefaultBackupImage, b.Volume, dir)
		if err != nil {
			return nil, err
		}
		first = displayCommand(c)
	}
	return []string{first, fmt.Sprintf("keep last %d backups of %s in %s", b.Keep, d.project, pdir)}, nil
}

// Restore runs the restore command of a backup step against an existing backup directory. The directory must
// be a backup of the project below the backup root.
func (e *Executor) Restore(ctx context.Context, project string, step config.ResolvedStep, backupDir string) (StepResult, error) {
	label := fmt.Sprintf("project=%s restore dir=%s", project, backupDir)
	run := func(ctx context.Context, s *stepRun, st config.ResolvedStep) error {
		if e.Runner == nil {
			return errors.New("executor has no runner")
		}
		if st.Backup == nil || strings.TrimSpace(st.Backup.RestoreCommand) == "" {
			return errors.New("backup step has no restore command")
		}
		if err := needDir(st); err != nil {
			return err
		}
		if err := e.checkBackupDir(project, backupDir); err != nil {
			return err
		}
		s.backupDir = backupDir
		_, err := s.run(ctx, Command{Name: shell, Args: []string{"-c", st.Backup.RestoreCommand}, Dir: st.Dir,
			Env: []string{backupDirEnv + "=" + backupDir}})
		return err
	}
	res, err := e.runBlock(ctx, project, label, step, run)
	if err != nil {
		return res, redactedError{msg: e.redactErr(err), err: err}
	}
	return res, nil
}

// checkBackupDir accepts only an existing plain backup directory directly below the project directory.
func (e *Executor) checkBackupDir(project, dir string) error {
	pdir, err := projectBackupDir(e.backupRoot(), project)
	if err != nil {
		return err
	}
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		if part == ".." {
			return fmt.Errorf("backup directory %q contains ..", dir)
		}
	}
	if filepath.Dir(dir) != pdir || !isBackupName(filepath.Base(dir)) {
		return fmt.Errorf("%q is not a backup directory of project %s", dir, project)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%q is not a directory", dir)
	}
	return nil
}
