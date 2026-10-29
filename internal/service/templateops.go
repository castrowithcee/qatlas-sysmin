package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

func (s *Service) registerTemplates() error {
	for op, h := range map[string]daemon.Handler{
		control.OpTemplatesList:   {Action: auditlog.OperationConfigRead, Func: s.handleTemplatesList},
		control.OpTemplatesShow:   {Action: auditlog.OperationConfigRead, Func: s.handleTemplatesShow},
		control.OpTemplatesDerive: {Action: auditlog.OperationConfigApply, Mutating: true, Func: s.handleTemplatesDerive},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

func templateSummary(t config.Template, origin string) control.TemplateSummary {
	source := control.TemplateSourceConfig
	if origin == control.TemplateBuiltin {
		source = control.TemplateSourceEmbedded
	}
	return control.TemplateSummary{Name: t.Name, Origin: origin, Source: source, Description: t.Description, Steps: len(t.Steps)}
}

// findTemplate returns a builtin or custom template and its origin.
func findTemplate(m *config.Model, name string) (config.Template, string, bool) {
	for _, t := range config.BuiltinTemplates() {
		if t.Name == name {
			return t, control.TemplateBuiltin, true
		}
	}
	for _, t := range m.Templates {
		if t.Name == name {
			return t, control.TemplateCustom, true
		}
	}
	return config.Template{}, "", false
}

func templateDetail(t config.Template, origin string) (control.TemplateDetail, error) {
	data, err := config.EncodeTemplate(t)
	if err != nil {
		return control.TemplateDetail{}, internalError("cannot encode a template", err)
	}
	return control.TemplateDetail{TemplateSummary: templateSummary(t, origin), File: string(data)}, nil
}

func (s *Service) handleTemplatesList(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	m, _, err := s.loadModel()
	if err != nil {
		return nil, err
	}
	out := control.TemplatesListResult{Templates: []control.TemplateSummary{}}
	for _, t := range config.BuiltinTemplates() {
		out.Templates = append(out.Templates, templateSummary(t, control.TemplateBuiltin))
	}
	custom := append([]config.Template(nil), m.Templates...)
	sort.Slice(custom, func(i, j int) bool { return custom[i].Name < custom[j].Name })
	for _, t := range custom {
		out.Templates = append(out.Templates, templateSummary(t, control.TemplateCustom))
	}
	return out, nil
}

func (s *Service) handleTemplatesShow(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	ref, err := decodeParams(params, func(r control.TemplateRef) string { return r.Name })
	if err != nil {
		return nil, err
	}
	m, _, err := s.loadModel()
	if err != nil {
		return nil, err
	}
	t, origin, ok := findTemplate(m, ref.Name)
	if !ok {
		return nil, &control.Error{Code: control.CodeNotFound}
	}
	return templateDetail(t, origin)
}

// handleTemplatesDerive stores a new custom template only. It never replaces a file, and it refuses a name
// that a project already refers to: such a project has no steps today, and the new template would change
// its approved state without a person approving it.
func (s *Service) handleTemplatesDerive(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.TemplateDeriveParams
	if json.Unmarshal(params, &p) != nil || !config.ValidName(p.From) || p.Name == "" || len(p.Name) > 64 {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	m, _, err := s.loadModel()
	if err != nil {
		return nil, err
	}
	src, _, ok := findTemplate(m, p.From)
	if !ok {
		return nil, &control.Error{Code: control.CodeNotFound}
	}
	if _, _, taken := findTemplate(m, p.Name); taken {
		return nil, &control.Error{Code: control.CodeExists}
	}
	for _, pr := range m.Projects {
		if pr.Template == p.Name {
			return nil, &control.Error{Code: control.CodeInUse}
		}
	}
	t := config.Template{Schema: config.CurrentSchema, Name: p.Name, Description: src.Description, Steps: src.Steps}
	if p.Description != "" {
		t.Description = p.Description
	}
	after := *m
	after.Templates = append(append([]config.Template(nil), m.Templates...), t)
	var found []config.Problem
	for _, pr := range config.Validate(&after, nil) {
		if pr.Template == t.Name {
			found = append(found, pr)
		}
	}
	if len(found) > 0 {
		return nil, &control.Error{Code: control.CodeInvalid, Problems: problemsOf(found)}
	}
	if _, err := s.store.SaveTemplate(t, config.RevisionAbsent); err != nil {
		if errors.Is(err, config.ErrConflict) {
			return nil, &control.Error{Code: control.CodeExists}
		}
		return nil, internalError("cannot save a template", err)
	}
	return templateDetail(t, control.TemplateCustom)
}
