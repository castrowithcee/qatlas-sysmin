package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

func (s *Service) registerCredentials() error {
	for op, h := range map[string]daemon.Handler{
		control.OpCredentialsList:   {Action: auditlog.OperationConfigRead, Func: s.handleCredentialsList},
		control.OpCredentialsSet:    {Action: auditlog.OperationCredentialChange, Func: s.handleCredentialsSet},
		control.OpCredentialsRemove: {Action: auditlog.OperationCredentialDelete, Func: s.handleCredentialsRemove},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

// vaultCode maps the error of a vault operation on a credential to a fixed code.
func vaultCode(err error) error {
	switch {
	case errors.Is(err, vault.ErrLocked), errors.Is(err, vault.ErrAbsent):
		return &control.Error{Code: control.CodeLocked}
	case errors.Is(err, vault.ErrExists):
		return &control.Error{Code: control.CodeExists}
	case errors.Is(err, vault.ErrNotFound):
		return &control.Error{Code: control.CodeNotFound}
	}
	return internalError("a vault operation failed", err)
}

// approvalTarget is the name an approval of the reference carries: a project, or the channels as one unit.
func approvalTarget(r approval.Reference) string {
	if r.Kind == approval.RefChannel {
		return approval.ChannelsName
	}
	return r.Name
}

func usesOf(refs []approval.Reference) []control.CredentialUse {
	out := make([]control.CredentialUse, 0, len(refs))
	for _, r := range refs {
		out = append(out, control.CredentialUse{Kind: string(r.Kind), Name: r.Name, Current: r.Current, Approved: r.Approved})
	}
	return out
}

// openByName returns the open approvals by the name they carry; an error leaves the map empty.
func (s *Service) openByName() map[string]approval.Item {
	out := map[string]approval.Item{}
	items, err := s.approvals.Open()
	if err != nil {
		slog.Warn("cannot read the open approvals", "error", err.Error())
		return out
	}
	for _, it := range items {
		out[it.Project] = it
	}
	return out
}

func (s *Service) handleCredentialsList(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	if s.vaultState() != control.VaultUnlocked {
		return nil, &control.Error{Code: control.CodeLocked}
	}
	infos, err := s.vault.List()
	if err != nil {
		return nil, vaultCode(err)
	}
	out := control.CredentialsListResult{Credentials: make([]control.CredentialSummary, 0, len(infos))}
	for _, in := range infos {
		refs, err := s.approvals.References(in.Name)
		if err != nil {
			return nil, vaultCode(err)
		}
		out.Credentials = append(out.Credentials, control.CredentialSummary{
			Name: in.Name, Kind: string(in.Kind), ID: in.ID, Uses: usesOf(refs),
		})
	}
	return out, nil
}

// credentialProblems checks the parameters of a set. Messages name fields, never values.
func credentialProblems(p control.CredentialSetParams) []control.Problem {
	var out []control.Problem
	add := func(field, code, msg string) {
		out = append(out, control.Problem{Field: field, Code: code, Message: msg, Hint: "Correct the request."})
	}
	if !config.ValidCredentialName(p.Name) {
		add("name", config.CodeInvalidName, fmt.Sprintf("credential name %q is not valid; use lower-case letters, digits, dots, underscores and hyphens, starting with a letter or digit", p.Name))
	}
	want := vault.FieldsOf(vault.Kind(p.Kind))
	if want == nil {
		kinds := make([]string, 0, len(vault.Kinds()))
		for _, k := range vault.Kinds() {
			kinds = append(kinds, string(k))
		}
		add("kind", config.CodeInvalidValue, fmt.Sprintf("credential kind %q is unknown (%s)", p.Kind, strings.Join(kinds, ", ")))
		return out
	}
	for _, f := range want {
		if p.Fields[f] == "" {
			add("fields."+f, config.CodeMissingField, fmt.Sprintf("a %s credential needs a value for %q", p.Kind, f))
		}
	}
	for f := range p.Fields {
		known := false
		for _, w := range want {
			known = known || w == f
		}
		if !known {
			add("fields", config.CodeFieldNotAllowed, fmt.Sprintf("a field that a %s credential does not have was given", p.Kind))
		}
	}
	return out
}

func (s *Service) handleCredentialsSet(_ context.Context, c *daemon.Conn, params json.RawMessage) (res any, err error) {
	var p control.CredentialSetParams
	uerr := json.Unmarshal(params, &p)
	clear(params) // the parameters carry the secret
	if uerr != nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	defer func() { c.Audit(p.Name, codeOfErr(err)) }()
	if p.Name == "" {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	if s.vaultState() != control.VaultUnlocked {
		return nil, &control.Error{Code: control.CodeLocked}
	}
	if probs := credentialProblems(p); len(probs) > 0 {
		return nil, &control.Error{Code: control.CodeInvalid, Problems: probs}
	}

	s.credMu.Lock()
	defer s.credMu.Unlock()
	infos, err := s.vault.List()
	if err != nil {
		return nil, vaultCode(err)
	}
	var existing *vault.Info
	for i := range infos {
		if infos[i].Name == p.Name {
			existing = &infos[i]
		}
	}
	out := control.CredentialResult{Name: p.Name}
	if existing == nil {
		out.Action = "created"
		out.ID, err = s.vault.Add(p.Name, vault.Kind(p.Kind), p.Fields)
	} else {
		if string(existing.Kind) != p.Kind {
			return nil, &control.Error{Code: control.CodeKindMismatch}
		}
		out.Action = "replaced"
		out.ID, err = s.vault.Replace(p.Name, p.Fields)
	}
	if err != nil {
		return nil, vaultCode(err)
	}
	// One use of the admin session per change, whether or not something names the credential.
	admin := s.adminCovers(c)
	refs, err := s.approvals.References(p.Name)
	if err != nil {
		slog.Warn("cannot determine the uses of a credential", "error", err.Error())
		out.State = control.StatePending
		return out, nil
	}
	targets := map[string]bool{}
	for _, r := range refs {
		if r.Current {
			targets[approvalTarget(r)] = true
		}
	}
	if admin {
		open := s.openByName()
		for _, t := range sortedKeys(targets) {
			it, ok := open[t]
			if !ok || !onlyCredential(it, p.Name) {
				continue
			}
			if err := s.approvals.Approve(t, it.Fingerprint); err != nil {
				slog.Warn("a credential change made in an admin session could not be approved", "error", err.Error())
				continue
			}
			out.Approved = append(out.Approved, t)
		}
	}
	out.Pending = s.stillOpen(targets)
	out.State = stateOfList(out.Pending)
	return out, nil
}

// onlyCredential reports whether the one open change of the approval is the vault id of the credential
// name. An approval with any other change, such as a project edit that is still open, is not covered.
func onlyCredential(it approval.Item, name string) bool {
	if it.Kind != approval.KindCredential || it.Unverified || len(it.Changes) == 0 {
		return false
	}
	for _, ch := range it.Changes {
		if ch.Path != "credentials."+name {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// stillOpen returns the targets that have an open approval now, sorted.
func (s *Service) stillOpen(targets map[string]bool) []string {
	open := s.openByName()
	var out []string
	for _, t := range sortedKeys(targets) {
		if _, ok := open[t]; ok {
			out = append(out, t)
		}
	}
	return out
}

func stateOfList(pending []string) string {
	if len(pending) > 0 {
		return control.StatePending
	}
	return control.StateApproved
}

func (s *Service) handleCredentialsRemove(_ context.Context, c *daemon.Conn, params json.RawMessage) (res any, err error) {
	var p control.CredentialRef
	if json.Unmarshal(params, &p) != nil || p.Name == "" {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	defer func() { c.Audit(p.Name, codeOfErr(err)) }()
	if s.vaultState() != control.VaultUnlocked {
		return nil, &control.Error{Code: control.CodeLocked}
	}

	s.credMu.Lock()
	defer s.credMu.Unlock()
	infos, err := s.vault.List()
	if err != nil {
		return nil, vaultCode(err)
	}
	id := ""
	for _, in := range infos {
		if in.Name == p.Name {
			id = in.ID
		}
	}
	if id == "" {
		return nil, &control.Error{Code: control.CodeNotFound}
	}
	admin := s.adminCovers(c)
	refs, err := s.approvals.References(p.Name)
	if err != nil {
		return nil, vaultCode(err)
	}
	if len(refs) > 0 && !admin {
		return nil, &control.Error{Code: control.CodeInUse, Problems: inUseProblems(refs)}
	}
	if err := s.vault.Delete(id); err != nil {
		return nil, vaultCode(err)
	}
	// Superseded values of the credential go with it unless an approved state still names them.
	s.approvals.PurgeSuperseded()
	targets := map[string]bool{}
	for _, r := range refs {
		targets[approvalTarget(r)] = true
	}
	out := control.CredentialResult{Name: p.Name, Action: "removed"}
	out.Pending = s.stillOpen(targets)
	out.State = stateOfList(out.Pending)
	return out, nil
}

func inUseProblems(refs []approval.Reference) []control.Problem {
	out := make([]control.Problem, 0, len(refs))
	for _, r := range refs {
		where := "the current configuration"
		switch {
		case r.Current && r.Approved:
			where = "the current configuration and the approved state"
		case r.Approved:
			where = "the approved state"
		}
		out = append(out, control.Problem{
			Field: string(r.Kind) + ":" + r.Name, Code: control.CodeInUse,
			Message: fmt.Sprintf("the %s %s uses the credential in %s", r.Kind, r.Name, where),
			Hint:    "Remove the use first, or remove the credential in an admin session.",
		})
	}
	return out
}
