package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDecodeRejects(t *testing.T) {
	var se *SchemaError
	if _, err := DecodeSettings([]byte("poll_interval: 1h\n")); !errors.As(err, &se) || se.Got != 0 {
		t.Errorf("missing schema: %v", err)
	}
	if _, err := DecodeSettings([]byte("schema: 2\nfuture_field: 1\n")); !errors.As(err, &se) || se.Got != 2 || !strings.Contains(err.Error(), "newer") {
		t.Errorf("newer schema must be reported as such: %v", err)
	}
	if _, err := DecodeProject([]byte("schema: 1\nname: a\ndirr: /x\n")); err == nil || !strings.Contains(err.Error(), "dirr") {
		t.Errorf("unknown field: %v", err)
	}
	if _, err := DecodeSettings([]byte("schema: 1\npoll_interval: soon\n")); err == nil {
		t.Error("bad duration")
	}
	if _, err := DecodeSettings([]byte("schema: 1\n---\nschema: 1\n")); err == nil {
		t.Error("second document")
	}
	if _, err := DecodeTemplate([]byte("")); err == nil {
		t.Error("empty file")
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	m := baseModel()
	m.Settings.PollInterval = Duration(90e9)
	data, err := EncodeSettings(m.Settings)
	if err != nil || !strings.Contains(string(data), "poll_interval: 90s") {
		t.Fatalf("%v\n%s", err, data)
	}
	back, err := DecodeSettings(data)
	if err != nil || !reflect.DeepEqual(back, m.Settings) {
		t.Errorf("settings: %v", err)
	}
	data, _ = EncodeProject(m.Projects[0])
	if p, err := DecodeProject(data); err != nil || !reflect.DeepEqual(p, m.Projects[0]) {
		t.Errorf("project: %v", err)
	}
	tpl := Template{Name: "mine", Steps: []Step{{Type: StepBackup, Volume: "v", Keep: 3}, {Type: StepCommand, Run: "true", Timeout: Duration(60e9)}}}
	data, _ = EncodeTemplate(tpl)
	back2, err := DecodeTemplate(data)
	tpl.Schema = 1
	if err != nil || !reflect.DeepEqual(back2, tpl) {
		t.Errorf("template: %v", err)
	}
}

func TestLoad(t *testing.T) {
	good := fstest.MapFS{
		"sysmin.yaml":         {Data: []byte("schema: 1\n")},
		"projects/site.yaml":  {Data: []byte("schema: 1\nname: site\ndir: /srv/example/site\ntemplate: git-tag\ngit:\n  url: https://git.example.com/a.git\n")},
		"templates/mine.yaml": {Data: []byte("schema: 1\nname: mine\nsteps:\n  - type: compose-up\n")},
	}
	m, err := Load(good)
	if err != nil || len(m.Projects) != 1 || len(m.Templates) != 1 {
		t.Fatalf("%v %+v", err, m)
	}
	if _, ok := m.Template("mine"); !ok {
		t.Error("custom template not found")
	}
	if _, ok := m.Template("image-tag"); !ok {
		t.Error("embedded template not found")
	}

	bad := fstest.MapFS{
		"projects/other.yaml": {Data: []byte("schema: 1\nname: site\n")},
		"projects/x.yml":      {Data: []byte("schema: 1\n")},
		"templates/t.yaml":    {Data: []byte("schema: 9\n")},
	}
	_, err = Load(bad)
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"sysmin.yaml", "does not match project name", "projects/x.yml", "newer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

func TestPlaceholderParsing(t *testing.T) {
	ok := map[string]string{"a {version} b": "a X b", "{{literal}}": "{literal}", "}}": "}", "plain": "plain"}
	for in, want := range ok {
		got, perr := substitute(in, false, func(string) (value, *phError) { return value{scalar: "X"}, nil })
		if perr != nil || got != want {
			t.Errorf("%q: %q %v", in, got, perr)
		}
	}
	for _, in := range []string{"{", "}", "{Version}", "{a b}", "{}", "${HOME}"} {
		if _, err := parseText(in); err == nil {
			t.Errorf("%q must not parse", in)
		}
	}
}
