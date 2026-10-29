package approval

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"sync"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

var (
	// ErrNotApproved reports a project without an approved state.
	ErrNotApproved = errors.New("the project has no approved state")
	// ErrIntegrity reports an approved snapshot that does not match the fingerprint in the vault. The
	// project counts as not approved until it is approved again.
	ErrIntegrity = errors.New("the approved state does not match the vault")
	// ErrChanged reports that the project changed after the state to approve was determined; nothing was
	// approved.
	ErrChanged = errors.New("the project changed since it was shown")
	// ErrNothingOpen reports an approval or discard of a project that has no open approval.
	ErrNothingOpen = errors.New("the project has no open approval")
)

// Manager joins the configuration, the vault and the state directory. The service is the only writer; the
// caller checks the admin session before it calls a method that changes anything. Every changing method
// fails with vault.ErrLocked, before it touches a file, unless the vault is unlocked.
type Manager struct {
	store    *config.Store
	vault    *vault.Vault
	stateDir string

	mu sync.Mutex
}

// New returns a Manager for the configuration in store, the fingerprints in v and the snapshots below
// stateDir.
func New(store *config.Store, v *vault.Vault, stateDir string) *Manager {
	return &Manager{store: store, vault: v, stateDir: stateDir}
}

// Approved is the approved state of one project.
type Approved struct {
	Name        string
	Fingerprint string
	// Project is the project file as it was approved.
	Project config.Project
	// Steps are the steps in effect when it was approved, those of the template included.
	Steps []config.Step
	// Credentials maps each credential name of the project to the vault id that was approved. A run uses
	// the entry with this id, not whatever carries the name now.
	Credentials map[string]string
}

// Runnable returns the project with its approved steps filled in, so that Resolve neither reads the live
// template nor the live project file.
func (a Approved) Runnable() config.Project {
	p := a.Project
	p.Steps = a.Steps
	return p
}

// credentialIDs maps credential names to vault ids. It fails with vault.ErrLocked.
func (m *Manager) credentialIDs() (map[string]string, error) {
	infos, err := m.vault.List()
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(infos))
	for _, i := range infos {
		ids[i.Name] = i.ID
	}
	return ids, nil
}

// Approved returns the approved state of a project if its snapshot matches the fingerprint in the vault.
// It fails with ErrNotApproved or ErrIntegrity otherwise, and with vault.ErrLocked while the vault is
// locked.
func (m *Manager) Approved(name string) (Approved, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	approvals, err := m.vault.Approvals()
	if err != nil {
		return Approved{}, err
	}
	return m.approved(name, approvals)
}

func (m *Manager) approved(name string, approvals map[string]string) (Approved, error) {
	want, ok := approvals[name]
	if !ok {
		return Approved{}, ErrNotApproved
	}
	s, err := m.verified(name, want)
	if err != nil {
		return Approved{}, err
	}
	return Approved{Name: name, Fingerprint: want, Project: s.project, Steps: s.steps, Credentials: s.credentials}, nil
}

// verified reads the snapshot of name and checks it against the fingerprint want.
func (m *Manager) verified(name, want string) (state, error) {
	if !config.ValidName(name) {
		return state{}, fmt.Errorf("invalid project name %q", name)
	}
	s, err := m.readSnapshot(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return state{}, fmt.Errorf("%w: the snapshot of %s is missing", ErrIntegrity, name)
		}
		return state{}, fmt.Errorf("%w: %v", ErrIntegrity, err)
	}
	got, err := s.fingerprint()
	if err != nil {
		return state{}, err
	}
	if got != want {
		return state{}, fmt.Errorf("%w: the snapshot of %s was changed", ErrIntegrity, name)
	}
	return s, nil
}

// ApprovedModel returns a copy of m in which every project is its approved state. Projects without a
// verified approved state are left out and reported in skipped, so a run never uses an unapproved file.
// Settings and templates are kept as they are.
func (m *Manager) ApprovedModel(model *config.Model) (approved *config.Model, skipped map[string]error, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	approvals, err := m.vault.Approvals()
	if err != nil {
		return nil, nil, err
	}
	out := &config.Model{Settings: model.Settings, Templates: model.Templates}
	skipped = map[string]error{}
	names := make([]string, 0, len(approvals))
	for n := range approvals {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if n == ChannelsName {
			continue
		}
		a, err := m.approved(n, approvals)
		if err != nil {
			skipped[n] = err
			continue
		}
		out.Projects = append(out.Projects, a.Runnable())
	}
	for _, p := range model.Projects {
		if _, ok := approvals[p.Name]; !ok {
			skipped[p.Name] = ErrNotApproved
		}
	}
	return out, skipped, nil
}

// Open lists every open approval, sorted by name: new, changed and deleted projects, the notification
// channels as one item named ChannelsName, and projects or channels whose credential is another vault entry than the approved one. A credential is open exactly when a project
// that was approved names it and its vault id differs from the approved id (or it was missing then and
// exists now, or the reverse); a project that is new is approved together with its credentials.
func (m *Manager) Open() ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	model, _, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	return m.open(model)
}

func (m *Manager) open(model *config.Model) ([]Item, error) {
	approvals, err := m.vault.Approvals()
	if err != nil {
		return nil, err
	}
	ids, err := m.credentialIDs()
	if err != nil {
		return nil, err
	}
	var items []Item
	current := map[string]bool{}
	for _, p := range model.Projects {
		current[p.Name] = true
		cur := stateOf(model, p, ids)
		fp, err := cur.fingerprint()
		if err != nil {
			return nil, err
		}
		want, known := approvals[p.Name]
		if known && want == fp {
			continue
		}
		item := Item{Kind: KindNew, Project: p.Name, Fingerprint: fp}
		var old *state
		if known {
			item.Kind = KindChanged
			if s, err := m.verified(p.Name, want); err == nil {
				old = &s
			} else {
				item.Unverified = true
			}
		}
		if item.Changes, err = diffStates(old, &cur); err != nil {
			return nil, err
		}
		if known && !item.Unverified && onlyCredentials(item.Changes) {
			item.Kind = KindCredential
		}
		items = append(items, item)
	}
	for name, want := range approvals {
		if current[name] || name == ChannelsName {
			continue
		}
		item := Item{Kind: KindDeleted, Project: name}
		var old *state
		if s, err := m.verified(name, want); err == nil {
			old = &s
		} else {
			item.Unverified = true
		}
		if item.Changes, err = diffStates(old, nil); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	ch, err := m.openChannels(model, approvals, ids)
	if err != nil {
		return nil, err
	}
	if ch != nil {
		items = append(items, *ch)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Project < items[j].Project })
	return items, nil
}

// Approve makes the current state of a project its approved state. expect is the Fingerprint of the Item
// that was shown; when it is not empty and the project differs now, nothing happens and ErrChanged is
// returned. A project whose file is gone is approved as deleted: its approval and snapshot are removed.
// It returns ErrNothingOpen when there is nothing to approve.
func (m *Manager) Approve(name, expect string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	model, _, err := m.store.Load()
	if err != nil {
		return err
	}
	approvals, err := m.vault.Approvals()
	if err != nil {
		return err
	}
	ids, err := m.credentialIDs()
	if err != nil {
		return err
	}
	return m.approve(model, approvals, ids, name, expect)
}

func (m *Manager) approve(model *config.Model, approvals, ids map[string]string, name, expect string) error {
	var err error
	if name == ChannelsName {
		err = m.approveChannels(model, approvals, ids, expect)
	} else {
		err = m.approveProject(model, approvals, ids, name, expect)
	}
	if err == nil {
		m.purgeSuperseded()
	}
	return err
}

func (m *Manager) approveProject(model *config.Model, approvals, ids map[string]string, name, expect string) error {
	p, exists := model.Project(name)
	if !exists {
		if _, known := approvals[name]; !known {
			return fmt.Errorf("%w: %s", ErrNothingOpen, name)
		}
		if expect != "" {
			return ErrChanged
		}
		// The approval goes first: without it the project is not approved even if the snapshot stays.
		if err := m.vault.DeleteApproval(name); err != nil {
			return err
		}
		return m.removeSnapshot(name)
	}
	cur := stateOf(model, *p, ids)
	fp, err := cur.fingerprint()
	if err != nil {
		return err
	}
	if expect != "" && expect != fp {
		return ErrChanged
	}
	if approvals[name] == fp {
		return fmt.Errorf("%w: %s", ErrNothingOpen, name)
	}
	// The snapshot goes first: if the vault write then fails, the old fingerprint no longer matches the
	// snapshot and the project stays unrunnable and open until it is approved again.
	if err := m.writeSnapshot(cur); err != nil {
		return err
	}
	return m.vault.SetApproval(name, fp)
}

// ApproveAll approves every open approval as Open lists it and returns the approved project names. A
// failure for one project does not stop the others; the errors are joined.
func (m *Manager) ApproveAll() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	model, _, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	items, err := m.open(model)
	if err != nil {
		return nil, err
	}
	approvals, err := m.vault.Approvals()
	if err != nil {
		return nil, err
	}
	ids, err := m.credentialIDs()
	if err != nil {
		return nil, err
	}
	var done []string
	var errs []error
	for _, it := range items {
		if err := m.approve(model, approvals, ids, it.Project, it.Fingerprint); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", it.Project, err))
			continue
		}
		done = append(done, it.Project)
	}
	return done, errors.Join(errs...)
}

// ApproveCurrent approves the project file a change of an admin session has just written. written is the
// revision the store returned for that write (config.RevisionAbsent after a delete); when the file has
// another revision by now, someone else touched it and ErrChanged is returned without approving anything.
// A change that leaves the project as approved is not an error.
func (m *Manager) ApproveCurrent(name string, written config.Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	approvals, err := m.vault.Approvals()
	if err != nil {
		return err
	}
	rev, err := m.store.ProjectRevision(name)
	if err != nil {
		return err
	}
	if rev != written {
		return ErrChanged
	}
	model, _, err := m.store.Load()
	if err != nil {
		return err
	}
	ids, err := m.credentialIDs()
	if err != nil {
		return err
	}
	err = m.approve(model, approvals, ids, name, "")
	if errors.Is(err, ErrNothingOpen) {
		return nil
	}
	return err
}

// Discard restores the project file from the approved state through the store, so revisions and atomic
// replacement apply: a changed file is overwritten with the approved one, a deleted file is written again,
// and a project that was never approved is removed; ErrNothingOpen reports a name that is neither approved
// nor a file. A difference that lies only in a credential's vault id, or in a template, cannot be
// undone by restoring the file; such an item stays open.
func (m *Manager) Discard(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.discard(name)
}

func (m *Manager) discard(name string) error {
	if name == ChannelsName {
		return m.discardChannels()
	}
	approvals, err := m.vault.Approvals()
	if err != nil {
		return err
	}
	rev, err := m.store.ProjectRevision(name)
	if err != nil {
		return err
	}
	a, err := m.approved(name, approvals)
	switch {
	case errors.Is(err, ErrNotApproved):
		if rev == config.RevisionAbsent {
			return fmt.Errorf("%w: %s", ErrNothingOpen, name)
		}
		return m.store.DeleteProject(name, rev)
	case err != nil:
		return err
	}
	_, err = m.store.SaveProject(a.Project, rev)
	return err
}

// DiscardAll discards every open approval as Open lists it and returns the project names handled. A
// failure for one project does not stop the others; the errors are joined.
func (m *Manager) DiscardAll() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	model, _, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	items, err := m.open(model)
	if err != nil {
		return nil, err
	}
	var done []string
	var errs []error
	for _, it := range items {
		if it.Kind == KindCredential {
			continue
		}
		if err := m.discard(it.Project); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", it.Project, err))
			continue
		}
		done = append(done, it.Project)
	}
	return done, errors.Join(errs...)
}
