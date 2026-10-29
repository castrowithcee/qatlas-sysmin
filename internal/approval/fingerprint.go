// Package approval decides which state of a project the service may run. A project file edited by hand,
// through the CLI or by a restored backup is stored but not effective: only a state a human approved is.
// The fingerprint of the approved state lives in the vault, a snapshot of it under the state directory, so
// runs keep using the approved state while the file deviates. The package never reads or prints secret
// values; credentials enter the fingerprint by their vault id only.
package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// label separates these fingerprints from every other hash of the same bytes.
const label = "sysmin project approval v1"

// Keys of the canonical document.
const (
	keyProject     = "project"
	keySteps       = "steps"
	keyCredentials = "credentials"
)

// state is everything a run depends on: the project as written, the steps that are in effect (the project's
// own, else those of its template) and the vault ids of the credentials it names.
type state struct {
	project     config.Project
	steps       []config.Step
	credentials map[string]string
}

// credentialNames lists the credentials a project refers to, sorted and without repeats.
func credentialNames(p config.Project) []string {
	set := map[string]bool{}
	for _, im := range p.Images {
		set[im.Credential] = true
	}
	if p.Git != nil {
		set[p.Git.Credential] = true
	}
	if p.Webhook != nil {
		set[p.Webhook.Secret] = true
	}
	delete(set, "")
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// effectiveSteps returns the steps a run of p executes. An unknown template yields none.
func effectiveSteps(m *config.Model, p config.Project) []config.Step {
	if len(p.Steps) > 0 || p.Template == "" {
		return p.Steps
	}
	if t, ok := m.Template(p.Template); ok {
		return t.Steps
	}
	return nil
}

// stateOf captures the current state of p. ids maps credential names to vault ids; a credential the vault
// does not hold has the empty id, so creating it later changes the fingerprint.
func stateOf(m *config.Model, p config.Project, ids map[string]string) state {
	s := state{project: p, steps: effectiveSteps(m, p), credentials: map[string]string{}}
	for _, n := range credentialNames(p) {
		s.credentials[n] = ids[n]
	}
	return s
}

// generic turns v into plain maps, slices and scalars through its YAML form. The YAML tags drop empty
// fields, so the form does not change when the model gains a field that stays unset.
func generic(v any) (any, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// canonical is the document the fingerprint covers, without the label.
func (s state) canonical() (map[string]any, error) {
	proj, err := generic(s.project)
	if err != nil {
		return nil, err
	}
	pm, ok := proj.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("project %q has no canonical form", s.project.Name)
	}
	delete(pm, "schema")
	steps, err := generic(s.steps)
	if err != nil {
		return nil, err
	}
	creds := map[string]any{}
	for n, id := range s.credentials {
		creds[n] = id
	}
	return map[string]any{keyProject: pm, keySteps: steps, keyCredentials: creds}, nil
}

// fingerprint is the SHA-256 of the labelled canonical JSON, as hex. JSON object keys are sorted, so the
// order and comments of the YAML file do not matter.
func (s state) fingerprint() (string, error) {
	doc, err := s.canonical()
	if err != nil {
		return "", err
	}
	doc["label"] = label
	data, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Fingerprint returns the fingerprint of project p in model m. ids maps credential names to vault ids.
func Fingerprint(m *config.Model, p config.Project, ids map[string]string) (string, error) {
	return stateOf(m, p, ids).fingerprint()
}
