package approval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// ChannelsName is the name under which the notification channels are approved as one unit. It is not a
// valid project name, so it never collides with a project.
const ChannelsName = "@channels"

// channelsLabel separates the channel fingerprint from every other hash of the same bytes.
const channelsLabel = "sysmin channels approval v1"

const channelsSnapshotFile = "channels" + snapshotExt

const keyChannels = "channels"

// channelState is everything sending depends on: all channels as written and the vault ids of the
// credentials they name.
type channelState struct {
	channels    []config.Channel
	credentials map[string]string
}

func channelStateOf(channels []config.Channel, ids map[string]string) channelState {
	s := channelState{channels: channels, credentials: map[string]string{}}
	for _, c := range channels {
		if c.Credential != "" {
			s.credentials[c.Credential] = ids[c.Credential]
		}
	}
	return s
}

// canonical keys the channels by name, so the order of the list does not matter and a difference is
// reported under the channel's name. A repeated name gets its list position appended, so no channel can
// hide behind another.
func (s channelState) canonical() (map[string]any, error) {
	chans := map[string]any{}
	for i, c := range s.channels {
		g, err := generic(c)
		if err != nil {
			return nil, err
		}
		key := c.Name
		if _, dup := chans[key]; dup {
			key += "#" + strconv.Itoa(i)
		}
		chans[key] = g
	}
	creds := map[string]any{}
	for n, id := range s.credentials {
		creds[n] = id
	}
	return map[string]any{keyChannels: chans, keyCredentials: creds}, nil
}

func (s channelState) fingerprint() (string, error) {
	doc, err := s.canonical()
	if err != nil {
		return "", err
	}
	doc["label"] = channelsLabel
	data, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type channelsSnapshotFileDoc struct {
	Schema      int               `yaml:"schema"`
	Channels    []config.Channel  `yaml:"channels,omitempty"`
	Credentials map[string]string `yaml:"credentials,omitempty"`
}

func (m *Manager) channelsSnapshotPath() string {
	return filepath.Join(m.stateDir, approvedDir, channelsSnapshotFile)
}

func (m *Manager) writeChannelsSnapshot(s channelState) error {
	data, err := yaml.Marshal(channelsSnapshotFileDoc{Schema: config.CurrentSchema, Channels: s.channels,
		Credentials: s.credentials})
	if err != nil {
		return fmt.Errorf("cannot encode the approved channels: %w", err)
	}
	return m.writeSnapshotFile(m.channelsSnapshotPath(), data)
}

func (m *Manager) readChannelsSnapshot() (channelState, error) {
	data, err := os.ReadFile(m.channelsSnapshotPath())
	if err != nil {
		return channelState{}, err
	}
	var f channelsSnapshotFileDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil || f.Schema != config.CurrentSchema {
		return channelState{}, errors.New("the approved channels are not readable")
	}
	if f.Credentials == nil {
		f.Credentials = map[string]string{}
	}
	return channelState{channels: f.Channels, credentials: f.Credentials}, nil
}

func (m *Manager) removeChannelsSnapshot() error {
	if err := os.Remove(m.channelsSnapshotPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove the approved channels: %w", err)
	}
	return nil
}

// verifiedChannels reads the channel snapshot and checks it against the fingerprint want.
func (m *Manager) verifiedChannels(want string) (channelState, error) {
	s, err := m.readChannelsSnapshot()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return channelState{}, fmt.Errorf("%w: the snapshot of the channels is missing", ErrIntegrity)
		}
		return channelState{}, fmt.Errorf("%w: %v", ErrIntegrity, err)
	}
	got, err := s.fingerprint()
	if err != nil {
		return channelState{}, err
	}
	if got != want {
		return channelState{}, fmt.Errorf("%w: the snapshot of the channels was changed", ErrIntegrity)
	}
	return s, nil
}

// ApprovedChannels is the approved state of the notification channels.
type ApprovedChannels struct {
	Fingerprint string
	// Channels are the channels as they were approved.
	Channels []config.Channel
	// Credentials maps each credential name of the channels to the vault id that was approved. Sending
	// uses the entry with this id, not whatever carries the name now.
	Credentials map[string]string
}

// ApprovedChannels returns the approved channels if their snapshot matches the fingerprint in the vault.
// It fails with ErrNotApproved when no channels are approved, with ErrIntegrity when the snapshot does not
// match, and with vault.ErrLocked while the vault is locked.
func (m *Manager) ApprovedChannels() (ApprovedChannels, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	approvals, err := m.vault.Approvals()
	if err != nil {
		return ApprovedChannels{}, err
	}
	want, ok := approvals[ChannelsName]
	if !ok {
		return ApprovedChannels{}, ErrNotApproved
	}
	s, err := m.verifiedChannels(want)
	if err != nil {
		return ApprovedChannels{}, err
	}
	return ApprovedChannels{Fingerprint: want, Channels: s.channels, Credentials: s.credentials}, nil
}

// openChannels returns the open approval of the channels, or nil when the channels are as approved. A
// channel is "new" when none was approved before, "deleted" when approved channels are gone from the
// settings; the item carries ChannelsName in Project.
func (m *Manager) openChannels(model *config.Model, approvals, ids map[string]string) (*Item, error) {
	want, known := approvals[ChannelsName]
	chans := model.Settings.Channels
	if len(chans) == 0 && !known {
		return nil, nil
	}
	var old *channelState
	unverified := false
	if known {
		if s, err := m.verifiedChannels(want); err == nil {
			old = &s
		} else {
			unverified = true
		}
	}
	item := &Item{Project: ChannelsName, Unverified: unverified}
	var cur *channelState
	if len(chans) == 0 {
		item.Kind = KindDeleted
	} else {
		c := channelStateOf(chans, ids)
		fp, err := c.fingerprint()
		if err != nil {
			return nil, err
		}
		if known && want == fp {
			return nil, nil
		}
		cur = &c
		item.Fingerprint = fp
		item.Kind = KindNew
		if known {
			item.Kind = KindChanged
		}
	}
	var o, c map[string]any
	var err error
	if old != nil {
		if o, err = old.canonical(); err != nil {
			return nil, err
		}
	}
	if cur != nil {
		if c, err = cur.canonical(); err != nil {
			return nil, err
		}
	}
	item.Changes = diff(o, c)
	if item.Kind == KindChanged && !unverified && onlyCredentials(item.Changes) {
		item.Kind = KindCredential
	}
	return item, nil
}

func (m *Manager) approveChannels(model *config.Model, approvals, ids map[string]string, expect string) error {
	chans := model.Settings.Channels
	if len(chans) == 0 {
		if _, known := approvals[ChannelsName]; !known {
			return fmt.Errorf("%w: channels", ErrNothingOpen)
		}
		if expect != "" {
			return ErrChanged
		}
		if err := m.vault.DeleteApproval(ChannelsName); err != nil {
			return err
		}
		return m.removeChannelsSnapshot()
	}
	cur := channelStateOf(chans, ids)
	fp, err := cur.fingerprint()
	if err != nil {
		return err
	}
	if expect != "" && expect != fp {
		return ErrChanged
	}
	if approvals[ChannelsName] == fp {
		return fmt.Errorf("%w: channels", ErrNothingOpen)
	}
	// Snapshot first, as for a project: a failing vault write leaves the channels open, not approved.
	if err := m.writeChannelsSnapshot(cur); err != nil {
		return err
	}
	return m.vault.SetApproval(ChannelsName, fp)
}

// ApproveCurrentChannels approves the channels an admin session has just written to the settings file.
// written is the revision SaveSettings returned for that write; when the file has another revision by
// now, someone else touched it and ErrChanged is returned without approving anything. A change that
// leaves the channels as approved is not an error.
func (m *Manager) ApproveCurrentChannels(written config.Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	approvals, err := m.vault.Approvals()
	if err != nil {
		return err
	}
	rev, err := m.store.SettingsRevision()
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
	err = m.approve(model, approvals, ids, ChannelsName, "")
	if errors.Is(err, ErrNothingOpen) {
		return nil
	}
	return err
}

// discardChannels writes the approved channels back into the settings file and leaves every other
// setting as it is; channels that were never approved are removed. Like a project, a difference that lies
// only in a credential's vault id cannot be undone this way.
func (m *Manager) discardChannels() error {
	approvals, err := m.vault.Approvals()
	if err != nil {
		return err
	}
	model, revs, err := m.store.Load()
	if err != nil {
		return err
	}
	settings := model.Settings
	if settings.Schema == 0 {
		settings.Schema = config.CurrentSchema
	}
	want, known := approvals[ChannelsName]
	if !known {
		if len(settings.Channels) == 0 {
			return fmt.Errorf("%w: channels", ErrNothingOpen)
		}
		settings.Channels = nil
	} else {
		s, err := m.verifiedChannels(want)
		if err != nil {
			return err
		}
		settings.Channels = s.channels
	}
	_, err = m.store.SaveSettings(settings, revs[config.SettingsPath()])
	return err
}

// RefKind says what refers to a credential.
type RefKind string

// The things that refer to a credential.
const (
	RefProject RefKind = "project"
	RefChannel RefKind = "channel"
)

// Reference is one project or channel that names a credential.
type Reference struct {
	Kind RefKind
	Name string
	// Current is true when the configuration as it is now names the credential, Approved when the
	// approved state does.
	Current  bool
	Approved bool
}

// References lists the projects and channels that name the credential, in the current configuration or in
// the approved state, sorted by kind and name. The approved state counts only where its snapshot verifies.
// Approved channels are one unit, so an approved channel is found by name inside the approved snapshot. It
// fails with vault.ErrLocked while the vault is locked.
func (m *Manager) References(credential string) ([]Reference, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	model, _, err := m.store.Load()
	if err != nil {
		return nil, err
	}
	approvals, err := m.vault.Approvals()
	if err != nil {
		return nil, err
	}
	found := map[[2]string]*Reference{}
	mark := func(kind RefKind, name string, approved bool) {
		k := [2]string{string(kind), name}
		r := found[k]
		if r == nil {
			r = &Reference{Kind: kind, Name: name}
			found[k] = r
		}
		if approved {
			r.Approved = true
		} else {
			r.Current = true
		}
	}
	for _, p := range model.Projects {
		for _, n := range credentialNames(p) {
			if n == credential {
				mark(RefProject, p.Name, false)
			}
		}
	}
	for _, c := range model.Settings.Channels {
		if c.Credential == credential {
			mark(RefChannel, c.Name, false)
		}
	}
	for name, want := range approvals {
		if name == ChannelsName {
			s, err := m.verifiedChannels(want)
			if err != nil {
				continue
			}
			for _, c := range s.channels {
				if c.Credential == credential {
					mark(RefChannel, c.Name, true)
				}
			}
			continue
		}
		s, err := m.verified(name, want)
		if err != nil {
			continue
		}
		if _, ok := s.credentials[credential]; ok {
			mark(RefProject, name, true)
		}
	}
	out := make([]Reference, 0, len(found))
	for _, r := range found {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// purgeSuperseded removes superseded vault entries that no verified approved state names any more. It
// is best effort and does nothing when an approved snapshot cannot be verified, since that snapshot may
// still name an entry; a leftover entry is harmless and removed with a later approval.
func (m *Manager) purgeSuperseded() {
	approvals, err := m.vault.Approvals()
	if err != nil {
		return
	}
	keep := map[string]bool{}
	for name, want := range approvals {
		var creds map[string]string
		if name == ChannelsName {
			s, err := m.verifiedChannels(want)
			if err != nil {
				return
			}
			creds = s.credentials
		} else {
			s, err := m.verified(name, want)
			if err != nil {
				return
			}
			creds = s.credentials
		}
		for _, id := range creds {
			keep[id] = true
		}
	}
	_, _ = m.vault.PurgeSuperseded(keep)
}
