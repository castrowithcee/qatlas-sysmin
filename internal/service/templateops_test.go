package service

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

const demoTemplate = `schema: 1
name: demo-custom
description: Example custom scenario.
steps:
  - type: compose-pull
  - type: compose-up
`

func templateEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, true, true)
	e.write(t, "sysmin.yaml", "schema: 1\n")
	e.write(t, "projects/demo-app.yaml", demoProject)
	e.write(t, "templates/demo-custom.yaml", demoTemplate)
	e.write(t, "templates/demo-empty.yaml", "schema: 1\nname: demo-empty\nsteps: []\n")
	e.start(t)
	return e
}

func TestTemplatesListAndShow(t *testing.T) {
	e := templateEnv(t)
	c := e.dial(t)
	got, err := c.Templates()
	if err != nil {
		t.Fatal(err)
	}
	origins := map[string]string{}
	for _, tm := range got {
		origins[tm.Name] = tm.Origin
	}
	want := map[string]string{"git-tag": "builtin", "image-tag": "builtin", "demo-custom": "custom", "demo-empty": "custom"}
	for n, o := range want {
		if origins[n] != o {
			t.Errorf("template %s origin = %q, want %q (%+v)", n, origins[n], o, got)
		}
	}
	if got[0].Origin != control.TemplateBuiltin || got[0].Source != control.TemplateSourceEmbedded || got[0].Steps == 0 {
		t.Errorf("first = %+v", got[0])
	}
	for _, tm := range got {
		if tm.Name == "demo-custom" && (tm.Source != control.TemplateSourceConfig || tm.Steps != 2 || tm.Description == "") {
			t.Errorf("custom = %+v", tm)
		}
	}

	d, err := c.Template("demo-custom")
	if err != nil || d.Origin != "custom" || !strings.Contains(d.File, "type: compose-up") || strings.Contains(d.File, e.cfg.ConfigDir) {
		t.Fatalf("show = %+v, %v", d, err)
	}
	if d, err = c.Template("image-tag"); err != nil || d.Origin != "builtin" || !strings.Contains(d.File, "name: image-tag") {
		t.Fatalf("show builtin = %+v, %v", d, err)
	}
	_, err = c.Template("nope")
	wantCode(t, err, control.CodeNotFound)
	_, err = c.Template("../etc/passwd")
	wantCode(t, err, control.CodeBadRequest)
}

func TestTemplatesDeriveNeedsAdminSession(t *testing.T) {
	e := templateEnv(t)
	_, err := e.dial(t).DeriveTemplate(control.TemplateDeriveParams{From: "image-tag", Name: "demo-copy"})
	wantCode(t, err, control.CodeAdminRequired)
	if e.exists("templates/demo-copy.yaml") {
		t.Fatal("the template was written without a session")
	}

	locked := newEnv(t, true, false)
	locked.start(t)
	_, err = locked.dial(t).DeriveTemplate(control.TemplateDeriveParams{From: "image-tag", Name: "demo-copy"})
	wantCode(t, err, control.CodeLocked)
}

func TestTemplatesDerive(t *testing.T) {
	e := templateEnv(t)
	admin := e.admin(t)
	if _, err := admin.ApproveAll(); err != nil {
		t.Fatal(err)
	}
	before, err := e.svc.approvals.Approved("demo-app")
	if err != nil {
		t.Fatal(err)
	}

	d, err := admin.DeriveTemplate(control.TemplateDeriveParams{From: "image-tag", Name: "demo-copy", Description: "Example copy."})
	if err != nil || d.Name != "demo-copy" || d.Origin != "custom" || d.Description != "Example copy." {
		t.Fatalf("derive = %+v, %v", d, err)
	}
	stored := e.read(t, "templates/demo-copy.yaml")
	src, _ := admin.Template("image-tag")
	if !strings.Contains(stored, "description: Example copy.") || strings.Count(stored, "- type:") != src.Steps {
		t.Fatalf("stored = %q", stored)
	}
	if tm, err := admin.Template("demo-copy"); err != nil || tm.Steps != src.Steps {
		t.Fatalf("show = %+v, %v", tm, err)
	}

	// Without a description the source's stays.
	d, err = admin.DeriveTemplate(control.TemplateDeriveParams{From: "demo-custom", Name: "demo-second"})
	if err != nil || d.Description != "Example custom scenario." {
		t.Fatalf("derive custom = %+v, %v", d, err)
	}

	// The approved state of a project and the open approvals are untouched.
	after, err := e.svc.approvals.Approved("demo-app")
	if err != nil || after.Fingerprint != before.Fingerprint {
		t.Fatalf("approved state changed: %+v, %v", after, err)
	}
	if items, err := admin.Approvals(); err != nil || len(items) != 0 {
		t.Fatalf("open approvals = %+v, %v", items, err)
	}
}

func TestTemplatesDeriveRefusals(t *testing.T) {
	e := templateEnv(t)
	for name, tc := range map[string]struct {
		p    control.TemplateDeriveParams
		code string
	}{
		"taken custom":    {control.TemplateDeriveParams{From: "image-tag", Name: "demo-custom"}, control.CodeExists},
		"taken builtin":   {control.TemplateDeriveParams{From: "image-tag", Name: "git-tag"}, control.CodeExists},
		"unknown source":  {control.TemplateDeriveParams{From: "nope", Name: "demo-copy"}, control.CodeNotFound},
		"no name":         {control.TemplateDeriveParams{From: "image-tag"}, control.CodeBadRequest},
		"bad source name": {control.TemplateDeriveParams{From: "../x", Name: "demo-copy"}, control.CodeBadRequest},
		"invalid name":    {control.TemplateDeriveParams{From: "image-tag", Name: "Bad_Name"}, control.CodeInvalid},
		"invalid content": {control.TemplateDeriveParams{From: "demo-empty", Name: "demo-copy"}, control.CodeInvalid},
		"used by project": {control.TemplateDeriveParams{From: "image-tag", Name: "image-tag-x"}, ""},
	} {
		admin := e.admin(t)
		if name == "used by project" {
			e.write(t, "projects/demo-user.yaml", strings.NewReplacer("demo-app", "demo-user", "image-tag", "image-tag-x").Replace(demoProject))
			tc.code = control.CodeInUse
		}
		_, err := admin.DeriveTemplate(tc.p)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		wantCode(t, err, tc.code)
		if tc.code == control.CodeInvalid {
			if len(problemsOfErr(t, err)) == 0 {
				t.Errorf("%s: no problems", name)
			}
		}
	}
	if e.exists("templates/demo-copy.yaml") || e.exists("templates/image-tag-x.yaml") || e.exists("templates/Bad_Name.yaml") {
		t.Fatal("a refused derive wrote a file")
	}
	if got := e.read(t, "templates/demo-custom.yaml"); got != demoTemplate {
		t.Fatalf("existing template changed: %q", got)
	}
}
