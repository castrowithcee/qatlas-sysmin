package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// LoadPath reads the configuration directory at dir.
func LoadPath(dir string) (*Model, error) { return Load(os.DirFS(dir)) }

// Load reads the configuration layout from fsys: sysmin.yaml, projects/*.yaml and templates/*.yaml. A file
// must be named after its entry. A missing sysmin.yaml loads as default settings without a revision. Load
// decodes only; Validate checks the content. All file errors are reported together.
func Load(fsys fs.FS) (*Model, error) {
	m, _, err := load(fsys)
	return m, err
}

// defaultSettings are the settings of a fresh installation, which has no sysmin.yaml; the first save
// creates it.
func defaultSettings() Settings { return Settings{Schema: CurrentSchema} }

// ReadSettings reads only sysmin.yaml from the configuration directory dir. A missing file gives the
// default settings; any other read or decode error is returned.
func ReadSettings(dir string) (Settings, error) {
	data, err := os.ReadFile(filepath.Join(dir, SettingsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return defaultSettings(), nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("%s: %w", SettingsFile, err)
	}
	return DecodeSettings(data)
}

// load is Load plus the revision of every file it read, keyed by layout path.
func load(fsys fs.FS) (*Model, Revisions, error) {
	var errs []error
	m := &Model{}
	revs := Revisions{}
	readFile := func(name string) ([]byte, error) {
		data, err := fs.ReadFile(fsys, name)
		if err == nil {
			revs[name] = revisionOf(data)
		}
		return data, err
	}

	data, err := readFile(SettingsFile)
	if errors.Is(err, fs.ErrNotExist) {
		m.Settings = defaultSettings()
	} else if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", SettingsFile, err))
	} else if m.Settings, err = DecodeSettings(data); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", SettingsFile, err))
	}

	files, ferr := listYAML(fsys, ProjectsDir)
	errs = append(errs, ferr...)
	for _, f := range files {
		data, err := readFile(f)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		p, err := DecodeProject(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		if f != ProjectPath(p.Name) {
			errs = append(errs, fmt.Errorf("%s: file name does not match project name %q (expected %s)", f, p.Name, ProjectPath(p.Name)))
		}
		m.Projects = append(m.Projects, p)
	}

	files, ferr = listYAML(fsys, TemplatesDir)
	errs = append(errs, ferr...)
	for _, f := range files {
		data, err := readFile(f)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		t, err := DecodeTemplate(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f, err))
			continue
		}
		if f != TemplatePath(t.Name) {
			errs = append(errs, fmt.Errorf("%s: file name does not match template name %q (expected %s)", f, t.Name, TemplatePath(t.Name)))
		}
		m.Templates = append(m.Templates, t)
	}

	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}
	return m, revs, nil
}

// listYAML returns the .yaml files of a directory in name order. A missing directory is empty; any other
// entry is an error so that a misspelled extension does not hide a file.
func listYAML(fsys fs.FS, dir string) ([]string, []error) {
	entries, err := fs.ReadDir(fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{fmt.Errorf("%s: %w", dir, err)}
	}
	var files []string
	var errs []error
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
			errs = append(errs, fmt.Errorf("%s/%s: unexpected directory", dir, name))
		case !strings.HasSuffix(name, fileExtension) || !e.Type().IsRegular():
			errs = append(errs, fmt.Errorf("%s/%s: not a regular %s file", dir, name, fileExtension))
		default:
			files = append(files, path.Join(dir, name))
		}
	}
	sort.Strings(files)
	return files, errs
}
