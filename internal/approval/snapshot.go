package approval

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// Layout of the approved snapshots below the state directory.
const (
	approvedDir  = "approved"
	projectsDir  = "projects"
	snapshotExt  = ".yaml"
	snapshotMode = 0o600
	dirMode      = 0o700
)

// snapshotFile is the YAML form of an approved state. It is trusted only after its fingerprint matches the
// one in the vault.
type snapshotFile struct {
	Schema      int               `yaml:"schema"`
	Project     config.Project    `yaml:"project"`
	Steps       []config.Step     `yaml:"steps,omitempty"`
	Credentials map[string]string `yaml:"credentials,omitempty"`
}

func (m *Manager) snapshotPath(name string) string {
	return filepath.Join(m.stateDir, approvedDir, projectsDir, name+snapshotExt)
}

func (m *Manager) writeSnapshot(s state) error {
	data, err := yaml.Marshal(snapshotFile{Schema: config.CurrentSchema, Project: s.project, Steps: s.steps,
		Credentials: s.credentials})
	if err != nil {
		return fmt.Errorf("cannot encode the approved state of %s: %w", s.project.Name, err)
	}
	path := m.snapshotPath(s.project.Name)
	dir := filepath.Dir(path)
	for _, d := range []string{filepath.Join(m.stateDir, approvedDir), dir} {
		if err := os.MkdirAll(d, dirMode); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
		if err := os.Chmod(d, dirMode); err != nil {
			return fmt.Errorf("cannot set the permissions of %s: %w", d, err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".sysmin-tmp-*")
	if err != nil {
		return fmt.Errorf("cannot write the approved state of %s: %w", s.project.Name, err)
	}
	name := tmp.Name()
	err = tmp.Chmod(snapshotMode)
	if err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("cannot write the approved state of %s: %w", s.project.Name, err)
	}
	return nil
}

// readSnapshot returns the stored state of name, or fs.ErrNotExist.
func (m *Manager) readSnapshot(name string) (state, error) {
	data, err := os.ReadFile(m.snapshotPath(name))
	if err != nil {
		return state{}, err
	}
	var f snapshotFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil || f.Schema != config.CurrentSchema || f.Project.Name != name {
		return state{}, fmt.Errorf("the approved state of %s is not readable", name)
	}
	if f.Credentials == nil {
		f.Credentials = map[string]string{}
	}
	return state{project: f.Project, steps: f.Steps, credentials: f.Credentials}, nil
}

func (m *Manager) removeSnapshot(name string) error {
	if err := os.Remove(m.snapshotPath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove the approved state of %s: %w", name, err)
	}
	return nil
}
