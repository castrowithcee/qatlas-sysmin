package approval

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const demoSettings = `schema: 1
poll_interval: 30m
webhook:
  listen: 127.0.0.1:9100
channels:
  - name: ops-hook
    type: webhook
    url: https://hooks.example.com/demo
    credential: demo-hook
`

const hookToken = "demo-hook-secret-value-0002"

func (e *env) hookCredential() string {
	e.t.Helper()
	id, err := e.vault.Add("demo-hook", vault.KindSecret, map[string]string{vault.FieldValue: hookToken})
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) channelItem() (Item, bool) {
	e.t.Helper()
	for _, it := range e.open() {
		if it.Project == ChannelsName {
			return it, true
		}
	}
	return Item{}, false
}

func (e *env) approveChannels() {
	e.t.Helper()
	if err := e.mgr.Approve(ChannelsName, ""); err != nil {
		e.t.Fatal(err)
	}
}

// channelEnv returns an env with an approved project and approved channels.
func channelEnv(t *testing.T) *env {
	e := newEnv(t)
	e.hookCredential()
	e.write("sysmin.yaml", demoSettings)
	e.approveDemo()
	e.approveChannels()
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() = %+v", items)
	}
	return e
}

func TestChannelsOpenUntilApproved(t *testing.T) {
	e := newEnv(t)
	e.hookCredential()
	e.write("sysmin.yaml", demoSettings)
	it, ok := e.channelItem()
	if !ok || it.Kind != KindNew || it.Fingerprint == "" {
		t.Fatalf("channel item = %+v, %v, want new", it, ok)
	}
	if _, ok := hasChange(it, "channels.ops-hook.url"); !ok {
		t.Fatalf("changes = %+v", it.Changes)
	}
	if _, err := e.mgr.ApprovedChannels(); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("ApprovedChannels() = %v, want ErrNotApproved", err)
	}
	if err := e.mgr.Approve(ChannelsName, "stale"); !errors.Is(err, ErrChanged) {
		t.Fatalf("Approve(stale) = %v, want ErrChanged", err)
	}
	if err := e.mgr.Approve(ChannelsName, it.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.channelItem(); ok {
		t.Fatal("channels still open after approval")
	}
	a, err := e.mgr.ApprovedChannels()
	if err != nil || len(a.Channels) != 1 || a.Channels[0].URL != "https://hooks.example.com/demo" || a.Credentials["demo-hook"] == "" {
		t.Fatalf("ApprovedChannels() = %+v, %v", a, err)
	}
	if err := e.mgr.Approve(ChannelsName, ""); !errors.Is(err, ErrNothingOpen) {
		t.Fatalf("second Approve() = %v, want ErrNothingOpen", err)
	}
}

func TestChannelHandEditStaysOpenAndApprovedStaysOld(t *testing.T) {
	e := channelEnv(t)
	e.write("sysmin.yaml", strings.Replace(demoSettings, "hooks.example.com/demo", "hooks.example.org/other", 1))
	it, ok := e.channelItem()
	if !ok || it.Kind != KindChanged {
		t.Fatalf("channel item = %+v, %v, want changed", it, ok)
	}
	c, ok := hasChange(it, "channels.ops-hook.url")
	if !ok || c.Old != "https://hooks.example.com/demo" || c.New != "https://hooks.example.org/other" {
		t.Fatalf("changes = %+v", it.Changes)
	}
	a, err := e.mgr.ApprovedChannels()
	if err != nil || a.Channels[0].URL != "https://hooks.example.com/demo" {
		t.Fatalf("ApprovedChannels() = %+v, %v, want the old URL", a, err)
	}
	e.approveChannels()
	a, err = e.mgr.ApprovedChannels()
	if err != nil || a.Channels[0].URL != "https://hooks.example.org/other" {
		t.Fatalf("ApprovedChannels() after approval = %+v, %v", a, err)
	}
}

func TestChannelOrderDoesNotOpenAnApproval(t *testing.T) {
	e := newEnv(t)
	e.hookCredential()
	two := strings.Replace(demoSettings, "channels:\n", "channels:\n  - name: alt-hook\n    type: webhook\n    url: https://hooks.example.com/alt\n", 1)
	e.write("sysmin.yaml", two)
	e.approveChannels()
	swapped := "schema: 1\npoll_interval: 30m\nwebhook:\n  listen: 127.0.0.1:9100\nchannels:\n" +
		"  - name: ops-hook\n    type: webhook\n    url: https://hooks.example.com/demo\n    credential: demo-hook\n" +
		"  - name: alt-hook\n    type: webhook\n    url: https://hooks.example.com/alt\n"
	e.write("sysmin.yaml", swapped)
	if _, ok := e.channelItem(); ok {
		t.Fatal("reordering channels opened an approval")
	}
}

func TestChannelDiscardRestoresChannelsOnly(t *testing.T) {
	e := channelEnv(t)
	e.write("sysmin.yaml", strings.Replace(demoSettings, "hooks.example.com/demo", "hooks.example.org/other", 1)+"  - name: extra\n    type: webhook\n    url: https://hooks.example.com/extra\n")
	// A setting outside the channels changes too and must survive the discard.
	e.write("sysmin.yaml", strings.Replace(e.read("sysmin.yaml"), "30m", "45m", 1))
	if err := e.mgr.Discard(ChannelsName); err != nil {
		t.Fatal(err)
	}
	model, _, err := e.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := model.Settings
	if len(s.Channels) != 1 || s.Channels[0].URL != "https://hooks.example.com/demo" {
		t.Fatalf("channels = %+v", s.Channels)
	}
	if s.PollInterval != config.Duration(45*60*1e9) || s.Webhook.Listen != "127.0.0.1:9100" {
		t.Fatalf("other settings changed: %+v", s)
	}
	if _, ok := e.channelItem(); ok {
		t.Fatal("channels still open after discard")
	}
}

func TestChannelsNeverApprovedDiscardedAndDeletedRestored(t *testing.T) {
	e := newEnv(t)
	e.hookCredential()
	e.write("sysmin.yaml", demoSettings)
	if err := e.mgr.Discard(ChannelsName); err != nil {
		t.Fatal(err)
	}
	model, _, _ := e.store.Load()
	if len(model.Settings.Channels) != 0 || model.Settings.PollInterval == 0 {
		t.Fatalf("settings after discard = %+v", model.Settings)
	}
	if err := e.mgr.Discard(ChannelsName); !errors.Is(err, ErrNothingOpen) {
		t.Fatalf("Discard() = %v, want ErrNothingOpen", err)
	}

	e.write("sysmin.yaml", demoSettings)
	e.approveChannels()
	e.write("sysmin.yaml", "schema: 1\npoll_interval: 30m\n")
	it, ok := e.channelItem()
	if !ok || it.Kind != KindDeleted || it.Fingerprint != "" {
		t.Fatalf("channel item = %+v, %v, want deleted", it, ok)
	}
	if err := e.mgr.Discard(ChannelsName); err != nil {
		t.Fatal(err)
	}
	model, _, _ = e.store.Load()
	if len(model.Settings.Channels) != 1 {
		t.Fatalf("channels = %+v", model.Settings.Channels)
	}
	e.write("sysmin.yaml", "schema: 1\n")
	e.approveChannels()
	if _, err := e.mgr.ApprovedChannels(); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("ApprovedChannels() = %v, want ErrNotApproved", err)
	}
	if _, err := os.Stat(e.mgr.channelsSnapshotPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot stays after a deleted approval: %v", err)
	}
}

func TestTamperedChannelSnapshotIsNotApproved(t *testing.T) {
	e := channelEnv(t)
	path := e.mgr.channelsSnapshotPath()
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "hooks.example.com", "evil.example.net", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.ApprovedChannels(); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("ApprovedChannels() = %v, want ErrIntegrity", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.ApprovedChannels(); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("ApprovedChannels() without snapshot = %v, want ErrIntegrity", err)
	}
}

func TestChannelsDoNotDisturbProjects(t *testing.T) {
	e := channelEnv(t)
	model, _, _ := e.store.Load()
	am, skipped, err := e.mgr.ApprovedModel(model)
	if err != nil || len(am.Projects) != 1 || len(skipped) != 0 {
		t.Fatalf("ApprovedModel() = %+v, %v, %v", am.Projects, skipped, err)
	}
}

func TestReplacedCredentialReopensProjectAndChannels(t *testing.T) {
	e := channelEnv(t)
	oldReg, _ := e.vault.GetByName("demo-registry")
	oldHook, _ := e.vault.GetByName("demo-hook")
	newReg, err := e.vault.Replace("demo-registry", map[string]string{vault.FieldUser: "demo-user", vault.FieldToken: "demo-token-value-0003"})
	if err != nil {
		t.Fatal(err)
	}
	newHook, err := e.vault.Replace("demo-hook", map[string]string{vault.FieldValue: "demo-hook-secret-value-0004"})
	if err != nil {
		t.Fatal(err)
	}
	items := e.open()
	if len(items) != 2 || items[0].Project != ChannelsName || items[0].Kind != KindCredential || items[1].Kind != KindCredential {
		t.Fatalf("Open() = %+v, want two credential items", items)
	}
	if c, ok := hasChange(items[0], "credentials.demo-hook"); !ok || c.Old != oldHook.ID || c.New != newHook {
		t.Fatalf("channel changes = %+v", items[0].Changes)
	}
	// The approved state keeps using the old ids, and the old entries are still readable by id.
	pa, _ := e.mgr.Approved("demo-app")
	ca, _ := e.mgr.ApprovedChannels()
	if pa.Credentials["demo-registry"] != oldReg.ID || ca.Credentials["demo-hook"] != oldHook.ID {
		t.Fatalf("approved ids = %v, %v", pa.Credentials, ca.Credentials)
	}
	if _, err := e.vault.Get(oldReg.ID); err != nil {
		t.Fatalf("Get(old registry) = %v", err)
	}
	// Discarding cannot undo a vault change.
	if err := e.mgr.Discard(ChannelsName); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.channelItem(); !ok {
		t.Fatal("a discard closed a credential item")
	}

	e.approveDemo()
	if _, err := e.vault.Get(oldReg.ID); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("Get(old registry) after approval = %v, want purged", err)
	}
	if _, err := e.vault.Get(oldHook.ID); err != nil {
		t.Fatalf("old hook purged while the channels still use it: %v", err)
	}
	e.approveChannels()
	if _, err := e.vault.Get(oldHook.ID); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("Get(old hook) after approval = %v, want purged", err)
	}
	pa, _ = e.mgr.Approved("demo-app")
	ca, _ = e.mgr.ApprovedChannels()
	if pa.Credentials["demo-registry"] != newReg || ca.Credentials["demo-hook"] != newHook {
		t.Fatalf("approved ids = %v, %v", pa.Credentials, ca.Credentials)
	}
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() = %+v", items)
	}
}

func TestReferences(t *testing.T) {
	e := channelEnv(t)
	refs, err := e.mgr.References("demo-hook")
	want := []Reference{{Kind: RefChannel, Name: "ops-hook", Current: true, Approved: true}}
	if err != nil || len(refs) != 1 || refs[0] != want[0] {
		t.Fatalf("References(demo-hook) = %+v, %v", refs, err)
	}
	// Dropped from the files but still part of the approved state.
	e.write("sysmin.yaml", "schema: 1\n")
	e.write("projects/demo-app.yaml", strings.Replace(demoProject, "    credential: demo-registry\n", "", 1))
	refs, _ = e.mgr.References("demo-hook")
	if len(refs) != 1 || refs[0].Current || !refs[0].Approved {
		t.Fatalf("References(demo-hook) = %+v, want approved only", refs)
	}
	refs, _ = e.mgr.References("demo-registry")
	if len(refs) != 1 || refs[0] != (Reference{Kind: RefProject, Name: "demo-app", Approved: true}) {
		t.Fatalf("References(demo-registry) = %+v", refs)
	}
	// Named only in the current files.
	e.write("sysmin.yaml", demoSettings)
	refs, _ = e.mgr.References("demo-hook")
	if len(refs) != 1 || !refs[0].Current || !refs[0].Approved {
		t.Fatalf("References(demo-hook) = %+v", refs)
	}
	if refs, _ := e.mgr.References("unused"); len(refs) != 0 {
		t.Fatalf("References(unused) = %+v", refs)
	}
	e.vault.Forget()
	if _, err := e.mgr.References("demo-hook"); !errors.Is(err, vault.ErrLocked) {
		t.Fatalf("References() locked = %v, want ErrLocked", err)
	}
}

func TestLockedVaultChangesNoChannel(t *testing.T) {
	e := channelEnv(t)
	e.write("sysmin.yaml", strings.Replace(demoSettings, "hooks.example.com/demo", "hooks.example.org/other", 1))
	before := e.read("sysmin.yaml")
	rev, _ := e.store.SettingsRevision()
	e.vault.Forget()
	checks := map[string]error{
		"Approve":        e.mgr.Approve(ChannelsName, ""),
		"Discard":        e.mgr.Discard(ChannelsName),
		"ApproveCurrent": e.mgr.ApproveCurrentChannels(rev),
	}
	_, checks["ApproveAll"] = e.mgr.ApproveAll()
	_, checks["DiscardAll"] = e.mgr.DiscardAll()
	_, checks["ApprovedChannels"] = e.mgr.ApprovedChannels()
	for name, err := range checks {
		if !errors.Is(err, vault.ErrLocked) {
			t.Errorf("%s locked = %v, want ErrLocked", name, err)
		}
	}
	if e.read("sysmin.yaml") != before {
		t.Fatal("a locked vault let the settings change")
	}
}

func TestApproveCurrentChannelsAfterWrite(t *testing.T) {
	e := channelEnv(t)
	model, revs, _ := e.store.Load()
	s := model.Settings
	s.Channels = append([]config.Channel(nil), s.Channels...)
	s.Channels[0].URL = "https://hooks.example.com/moved"
	rev, err := e.store.SaveSettings(s, revs[config.SettingsPath()])
	if err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.ApproveCurrentChannels(rev); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.channelItem(); ok {
		t.Fatal("channels open after ApproveCurrentChannels")
	}
	if err := e.mgr.ApproveCurrentChannels(rev); err != nil {
		t.Fatalf("repeat = %v, want no error", err)
	}
	e.write("sysmin.yaml", demoSettings) // someone edits in between
	if err := e.mgr.ApproveCurrentChannels(rev); !errors.Is(err, ErrChanged) {
		t.Fatalf("ApproveCurrentChannels() = %v, want ErrChanged", err)
	}
	if _, ok := e.channelItem(); !ok {
		t.Fatal("the foreign edit was approved")
	}
}

func TestApproveAllAndDiscardAllIncludeChannels(t *testing.T) {
	e := newEnv(t)
	e.hookCredential()
	e.write("sysmin.yaml", demoSettings)
	done, err := e.mgr.ApproveAll()
	if err != nil || len(done) != 2 || done[0] != ChannelsName && done[1] != ChannelsName {
		t.Fatalf("ApproveAll() = %v, %v", done, err)
	}
	e.write("sysmin.yaml", strings.Replace(demoSettings, "hooks.example.com/demo", "hooks.example.org/other", 1))
	done, err = e.mgr.DiscardAll()
	if err != nil || len(done) != 1 || done[0] != ChannelsName {
		t.Fatalf("DiscardAll() = %v, %v", done, err)
	}
	if _, ok := e.channelItem(); ok {
		t.Fatal("channels still open")
	}
}

func TestChannelSnapshotPermissionsAndNoSecrets(t *testing.T) {
	e := channelEnv(t)
	path := e.mgr.channelsSnapshotPath()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, %v", info, err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), hookToken) {
		t.Fatal("snapshot holds a secret value")
	}
	// Neither a replaced credential's diff nor an error names a value.
	if _, err := e.vault.Replace("demo-hook", map[string]string{vault.FieldValue: "demo-hook-secret-value-0005"}); err != nil {
		t.Fatal(err)
	}
	it, _ := e.channelItem()
	for _, c := range it.Changes {
		if strings.Contains(c.Old+c.New, "secret-value") {
			t.Fatalf("diff names a secret: %+v", c)
		}
	}
	err = e.mgr.Approve(ChannelsName, "stale")
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("Approve() = %v", err)
	}
}
