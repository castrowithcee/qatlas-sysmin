package vault

import (
	"fmt"
	"strings"
)

// Approvals returns the fingerprints of the approved projects, by project name. It fails with ErrLocked
// unless the key is held. The map is a copy.
func (v *Vault) Approvals() (map[string]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(v.doc.Approvals))
	for k, f := range v.doc.Approvals {
		out[k] = f
	}
	return out, nil
}

// SetApproval records fingerprint as the approved state of project. Fingerprints live in the vault so that
// whoever can edit the configuration files cannot approve its own change. It needs the key.
func (v *Vault) SetApproval(project, fingerprint string) error {
	if strings.TrimSpace(project) == "" || fingerprint == "" {
		return fmt.Errorf("an approval needs a project name and a fingerprint")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return err
	}
	return v.mutate(func(d *document) error {
		if d.Approvals == nil {
			d.Approvals = map[string]string{}
		}
		d.Approvals[project] = fingerprint
		return nil
	})
}

// DeleteApproval removes the approval of project; a project without one is not an error.
func (v *Vault) DeleteApproval(project string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return err
	}
	return v.mutate(func(d *document) error {
		delete(d.Approvals, project)
		return nil
	})
}
