package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/filelock"
)

// Permissions of everything the store creates: the group sysmin may read the configuration.
const (
	dirMode  = 0o750
	fileMode = 0o640
)

// Names under the root that are not part of the layout Load reads.
const (
	lockFile  = ".lock"
	backupDir = ".backups"
)

// Revision identifies the exact bytes of one configuration file as Load read them. It is opaque; callers
// keep it and hand it back when saving. RevisionAbsent stands for a file that does not exist.
type Revision string

// RevisionAbsent is the revision of a file that does not exist yet.
const RevisionAbsent Revision = ""

func revisionOf(data []byte) Revision {
	sum := sha256.Sum256(data)
	return Revision(hex.EncodeToString(sum[:]))
}

// Revisions maps layout paths (SettingsPath, ProjectPath, TemplatePath) to the revision of the file read.
// A path that is not in the map is absent.
type Revisions map[string]Revision

// ErrConflict is matched by errors.Is for a *ConflictError.
var ErrConflict = errors.New("the configuration changed since it was read")

// ConflictError reports that a file no longer has the content a change was based on, so nothing was
// written. The caller reloads and decides again; nothing is merged.
type ConflictError struct {
	Path string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s changed since it was read; reload it and repeat the change", e.Path)
}

// Is makes errors.Is(err, ErrConflict) true.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// Store reads and writes a configuration directory. Revisions are kept per file, so a change to one project
// does not conflict with a change to another. The store does not validate content; callers run Validate.
type Store struct {
	root string
}

// NewStore returns a store for the configuration directory at root. Nothing is created until a write.
func NewStore(root string) *Store { return &Store{root: root} }

// Load reads the whole configuration. Files of an older schema version are first migrated: the migrated
// file replaces the old one atomically and the previous bytes are kept under .backups. Files of a newer
// version are left untouched and reported as *SchemaError. The revisions belong to the bytes decoded.
func (s *Store) Load() (*Model, Revisions, error) {
	if err := s.migrateAll(); err != nil {
		return nil, nil, err
	}
	return load(os.DirFS(s.root))
}

// SaveSettings writes sysmin.yaml if it still has revision base and returns the new revision.
func (s *Store) SaveSettings(v Settings, base Revision) (Revision, error) {
	if err := checkWriteSchema(v.Schema); err != nil {
		return base, err
	}
	data, err := EncodeSettings(v)
	if err != nil {
		return base, err
	}
	return s.save(SettingsPath(), data, base)
}

// SaveProject writes the project file if it still has revision base and returns the new revision.
func (s *Store) SaveProject(v Project, base Revision) (Revision, error) {
	if !nameRe.MatchString(v.Name) {
		return base, fmt.Errorf("invalid project name %q", v.Name)
	}
	if err := checkWriteSchema(v.Schema); err != nil {
		return base, err
	}
	data, err := EncodeProject(v)
	if err != nil {
		return base, err
	}
	return s.save(ProjectPath(v.Name), data, base)
}

// SaveTemplate writes the template file if it still has revision base and returns the new revision.
func (s *Store) SaveTemplate(v Template, base Revision) (Revision, error) {
	if !nameRe.MatchString(v.Name) {
		return base, fmt.Errorf("invalid template name %q", v.Name)
	}
	if err := checkWriteSchema(v.Schema); err != nil {
		return base, err
	}
	data, err := EncodeTemplate(v)
	if err != nil {
		return base, err
	}
	return s.save(TemplatePath(v.Name), data, base)
}

// DeleteProject removes the project file if it still has revision base.
func (s *Store) DeleteProject(name string, base Revision) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid project name %q", name)
	}
	return s.remove(ProjectPath(name), base)
}

// DeleteTemplate removes the template file if it still has revision base.
func (s *Store) DeleteTemplate(name string, base Revision) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid template name %q", name)
	}
	return s.remove(TemplatePath(name), base)
}

func checkWriteSchema(v int) error {
	if v != 0 && v != schemaVersion {
		return &SchemaError{Got: v, Supported: schemaVersion}
	}
	return nil
}

// save replaces rel under the lock, after checking that it still has revision base.
func (s *Store) save(rel string, data []byte, base Revision) (Revision, error) {
	unlock, err := s.lock()
	if err != nil {
		return base, err
	}
	defer unlock()

	cur, _, err := s.current(rel)
	if err != nil {
		return base, err
	}
	if cur != base {
		return base, &ConflictError{Path: rel}
	}
	if err := s.replace(rel, data); err != nil {
		return base, err
	}
	return revisionOf(data), nil
}

func (s *Store) remove(rel string, base Revision) error {
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()

	cur, _, err := s.current(rel)
	if err != nil {
		return err
	}
	if cur != base {
		return &ConflictError{Path: rel}
	}
	if cur == RevisionAbsent {
		return fmt.Errorf("%s: %w", rel, fs.ErrNotExist)
	}
	if err := os.Remove(s.abs(rel)); err != nil {
		return err
	}
	return syncDir(filepath.Dir(s.abs(rel)))
}

// abs maps a layout path, built only from fixed names and validated entry names, below the root.
func (s *Store) abs(rel string) string { return filepath.Join(s.root, filepath.FromSlash(rel)) }

// current returns the revision and bytes of rel; a missing file is RevisionAbsent.
func (s *Store) current(rel string) (Revision, []byte, error) {
	data, err := os.ReadFile(s.abs(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return RevisionAbsent, nil, nil
	}
	if err != nil {
		return RevisionAbsent, nil, fmt.Errorf("cannot read %s: %w", rel, err)
	}
	return revisionOf(data), data, nil
}

// lock takes the cross-process write lock on a stable lock file in the root. The lock file is never
// replaced or removed, so the lock stays valid while the configuration files are replaced by rename.
func (s *Store) lock() (func(), error) {
	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", s.root, err)
	}
	path := filepath.Join(s.root, lockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, fileMode)
	if err != nil {
		return nil, fmt.Errorf("cannot open the lock file %s: %w", path, err)
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}

// replace atomically writes data to rel: temporary file in the target directory, fsync, rename, directory
// fsync. A failure leaves no temporary file behind.
func (s *Store) replace(rel string, data []byte) error {
	target := s.abs(rel)
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".sysmin-tmp-*")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	if err := writeAndSync(tmp, data); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("cannot write %s: %w", rel, err)
	}
	if err := os.Rename(name, target); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("cannot replace %s: %w", rel, err)
	}
	return syncDir(dir)
}

// writeAndSync writes data, sets the file mode, syncs and closes f.
func writeAndSync(f *os.File, data []byte) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Chmod(fileMode)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// migrateAll upgrades every file of an older schema version. It takes the lock and writes only when at
// least one file needs it.
func (s *Store) migrateAll() error {
	files := s.candidates()
	var pending []string
	for _, rel := range files {
		if data, err := os.ReadFile(s.abs(rel)); err == nil {
			if _, ok := needsMigration(data); ok {
				pending = append(pending, rel)
			}
		}
	}
	if len(pending) == 0 {
		return nil
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	var errs []error
	for _, rel := range pending {
		if err := s.migrateFile(rel); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rel, err))
		}
	}
	return errors.Join(errs...)
}

// candidates lists the layout files that may exist; entries Load would reject are skipped here and
// reported by Load.
func (s *Store) candidates() []string {
	fsys := os.DirFS(s.root)
	files := []string{SettingsPath()}
	for _, dir := range []string{ProjectsDir, TemplatesDir} {
		listed, _ := listYAML(fsys, dir)
		for _, f := range listed {
			base := strings.TrimSuffix(f[len(dir)+1:], fileExtension)
			if nameRe.MatchString(base) {
				files = append(files, f)
			}
		}
	}
	return files
}

// migrateFile re-reads rel under the lock, keeps the old bytes as a backup and writes the migrated file.
// The result must decode in its kind; otherwise nothing is changed.
func (s *Store) migrateFile(rel string) error {
	_, data, err := s.current(rel)
	if err != nil {
		return err
	}
	from, ok := needsMigration(data)
	if !ok {
		return nil
	}
	out, err := migrate(data, from)
	if err != nil {
		return err
	}
	switch {
	case rel == SettingsPath():
		v, derr := DecodeSettings(out)
		if derr != nil {
			return derr
		}
		out, err = EncodeSettings(v)
	case strings.HasPrefix(rel, ProjectsDir+"/"):
		v, derr := DecodeProject(out)
		if derr != nil {
			return derr
		}
		out, err = EncodeProject(v)
	default:
		v, derr := DecodeTemplate(out)
		if derr != nil {
			return derr
		}
		out, err = EncodeTemplate(v)
	}
	if err != nil {
		return err
	}
	if err := s.backup(rel, from, data); err != nil {
		return err
	}
	return s.replace(rel, out)
}

// backup keeps the exact previous bytes of rel as .backups/<path with / as _>.v<schema>.<UTC time>.
func (s *Store) backup(rel string, schema int, data []byte) error {
	dir := filepath.Join(s.root, backupDir)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	base := fmt.Sprintf("%s.v%d.%s", strings.ReplaceAll(rel, "/", "_"), schema, time.Now().UTC().Format("20060102T150405Z"))
	for i := 0; ; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot create the backup of %s: %w", rel, err)
		}
		if err := writeAndSync(f, data); err != nil {
			_ = os.Remove(f.Name())
			return fmt.Errorf("cannot write the backup of %s: %w", rel, err)
		}
		return syncDir(dir)
	}
}

// ValidName reports whether name is usable as a project or template name and so as a file name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// ProjectRevision returns the revision of the project file as it is now, RevisionAbsent when it does not
// exist. It reads the one file only, so it works while other files of the configuration are broken.
func (s *Store) ProjectRevision(name string) (Revision, error) {
	if !nameRe.MatchString(name) {
		return RevisionAbsent, fmt.Errorf("invalid project name %q", name)
	}
	rev, _, err := s.current(ProjectPath(name))
	return rev, err
}
