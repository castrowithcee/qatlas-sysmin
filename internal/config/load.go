package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

// LoadPath reads the configuration directory at dir.
func LoadPath(dir string) (*Model, error) { return Load(os.DirFS(dir)) }

// Load reads the configuration layout from fsys: sysmin.yaml, projects/*.yaml and templates/*.yaml. A file
// must be named after its entry. Load decodes only; Validate checks the content. All file errors are
// reported together.
func Load(fsys fs.FS) (*Model, error) {
	var errs []error
	m := &Model{}

	data, err := fs.ReadFile(fsys, SettingsFile)
	if err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", SettingsFile, err))
	} else if m.Settings, err = DecodeSettings(data); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", SettingsFile, err))
	}

	files, ferr := listYAML(fsys, ProjectsDir)
	errs = append(errs, ferr...)
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
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
		data, err := fs.ReadFile(fsys, f)
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
		return nil, errors.Join(errs...)
	}
	return m, nil
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
