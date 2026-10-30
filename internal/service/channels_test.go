package service

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

func opsHook() control.ChannelParams {
	return control.ChannelParams{Name: "ops-hook", Type: "webhook", URL: "https://hooks.example.com/demo"}
}

func opsMail() control.ChannelParams {
	return control.ChannelParams{
		Name: "ops-mail", Type: "smtp", Host: "smtp.example.com", From: "sysmin@example.com",
		To: []string{"ops@example.com"}, Security: "starttls", Credential: "demo-smtp",
	}
}

func stateOfChannel(t *testing.T, list []control.ChannelSummary, name string) string {
	t.Helper()
	for _, ch := range list {
		if ch.Name == name {
			return ch.State
		}
	}
	t.Fatalf("no channel %s in %+v", name, list)
	return ""
}

func TestChannelChangeWithoutSessionStaysPending(t *testing.T) {
	e := approvedEnv(t)
	c := e.dial(t)
	res, err := c.AddChannel(opsHook())
	if err != nil || res.State != control.StatePending || res.Project != approval.ChannelsName || res.Revision == "" {
		t.Fatalf("add = %+v, %v", res, err)
	}
	if got := openOf(t, c, approval.ChannelsName); got.Kind != "new" {
		t.Fatalf("summary = %+v", got)
	}
	list, err := c.Channels()
	if err != nil || len(list) != 1 || list[0].URL != "https://hooks.example.com/demo" || list[0].State != control.StatePending {
		t.Fatalf("list = %+v, %v", list, err)
	}

	// Approving makes it part of the approved state; the next one is pending beside it.
	if _, err := approveAll(t, e.admin(t)); err != nil {
		t.Fatal(err)
	}
	if list, _ = c.Channels(); stateOfChannel(t, list, "ops-hook") != control.StateApproved {
		t.Fatalf("list = %+v", list)
	}
	second := opsHook()
	second.Name = "ops-two"
	if res, err := c.AddChannel(second); err != nil || res.State != control.StatePending {
		t.Fatalf("add = %+v, %v", res, err)
	}
	list, _ = c.Channels()
	if stateOfChannel(t, list, "ops-hook") != control.StateApproved || stateOfChannel(t, list, "ops-two") != control.StatePending {
		t.Fatalf("list = %+v", list)
	}

	res, err = c.RemoveChannel("ops-two")
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("remove of an unapproved channel = %+v, %v", res, err)
	}
	res, err = c.RemoveChannel("ops-hook")
	if err != nil || res.State != control.StatePending {
		t.Fatalf("remove of an approved channel = %+v, %v", res, err)
	}
	if got := openOf(t, c, approval.ChannelsName); got.Kind != "deleted" {
		t.Fatalf("summary = %+v", got)
	}
	wantCode(t, func() error { _, err := c.RemoveChannel("ops-hook"); return err }(), control.CodeNotFound)
}

func TestChannelChangeInAdminSessionIsApproved(t *testing.T) {
	e := approvedEnv(t)
	adm := e.admin(t)
	res, err := adm.AddChannel(opsHook())
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("add = %+v, %v", res, err)
	}
	if _, err := e.svc.approvals.ApprovedChannels(); err != nil {
		t.Fatalf("approved channels: %v", err)
	}
	wantCode(t, func() error { _, err := adm.AddChannel(opsHook()); return err }(), control.CodeExists)
	res, err = adm.RemoveChannel("ops-hook")
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if list, _ := adm.Channels(); len(list) != 0 {
		t.Fatalf("list = %+v", list)
	}
}

func TestChannelInvalidChangeIsRefusedWithProblems(t *testing.T) {
	e := approvedEnv(t)
	c := e.dial(t)
	bad := opsMail() // names a credential the vault does not know
	probs := problemsOfErr(t, func() error { _, err := c.AddChannel(bad); return err }())
	if len(probs) != 1 || probs[0].Code != "unknown-credential" || probs[0].Field != "channels[0].credential" {
		t.Fatalf("problems = %+v", probs)
	}
	noURL := control.ChannelParams{Name: "ops-hook", Type: "webhook"}
	probs = problemsOfErr(t, func() error { _, err := c.AddChannel(noURL); return err }())
	if len(probs) != 1 || probs[0].Code != "missing-field" {
		t.Fatalf("problems = %+v", probs)
	}
	withLogin := &url.URL{Scheme: "https", User: url.UserPassword("user", "pass"), Host: "hooks.example.com", Path: "/x"}
	secretURL := control.ChannelParams{Name: "ops-hook", Type: "webhook", URL: withLogin.String()}
	problemsOfErr(t, func() error { _, err := c.AddChannel(secretURL); return err }())
	wantCode(t, func() error {
		_, err := c.AddChannel(control.ChannelParams{Name: "Bad Name", Type: "slack"})
		return err
	}(), control.CodeBadRequest)
	if list, _ := c.Channels(); len(list) != 0 {
		t.Fatalf("an invalid channel was stored: %+v", list)
	}
	// A problem in another channel does not block a valid one.
	e.write(t, "sysmin.yaml", "schema: 1\nchannels:\n  - name: broken\n    type: slack\n")
	if res, err := c.AddChannel(opsHook()); err != nil || res.State != control.StatePending {
		t.Fatalf("add beside a broken channel = %+v, %v", res, err)
	}
}

func TestChannelOperationsNeedAnUnlockedVault(t *testing.T) {
	e := newEnv(t, true, false)
	e.write(t, "sysmin.yaml", "schema: 1\n")
	e.start(t)
	c := e.dial(t)
	wantCode(t, func() error { _, err := c.AddChannel(opsHook()); return err }(), control.CodeLocked)
	wantCode(t, func() error { _, err := c.RemoveChannel("ops-hook"); return err }(), control.CodeLocked)
	if strings.Contains(e.read(t, "sysmin.yaml"), "channels") {
		t.Fatal("a channel was stored while the vault was locked")
	}
	if list, err := c.Channels(); err != nil || len(list) != 0 {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestCredentialChangeOpensTheChannelsThatUseIt(t *testing.T) {
	e := approvedEnv(t)
	adm := e.admin(t)
	smtp := control.CredentialSetParams{Name: "demo-smtp", Kind: "smtp", Fields: map[string]string{vault.FieldUser: "demo-user", vault.FieldPassword: demoSecret}}
	if _, err := adm.SetCredential(smtp); err != nil {
		t.Fatal(err)
	}
	if res, err := adm.AddChannel(opsMail()); err != nil || res.State != control.StateApproved {
		t.Fatalf("add = %+v, %v", res, err)
	}
	if use := credentialOf(t, adm, "demo-smtp").Uses; len(use) != 1 || use[0].Kind != "channel" || use[0].Name != "ops-mail" || !use[0].Current || !use[0].Approved {
		t.Fatalf("uses = %+v", use)
	}

	c := e.dial(t)
	smtp.Fields[vault.FieldPassword] = newSecret
	res, err := c.SetCredential(smtp)
	if err != nil || res.State != control.StatePending || !slices.Equal(res.Pending, []string{approval.ChannelsName}) {
		t.Fatalf("replace = %+v, %v", res, err)
	}
	if got := openOf(t, c, approval.ChannelsName); got.Kind != "credential" {
		t.Fatalf("summary = %+v", got)
	}
	if list, _ := c.Channels(); stateOfChannel(t, list, "ops-mail") != control.StatePending {
		t.Fatalf("list = %+v", list)
	}
	// Removing the credential is refused while the channel names it.
	wantCode(t, func() error { _, err := c.RemoveCredential("demo-smtp"); return err }(), control.CodeInUse)

	// In a session the same change is approved.
	smtp.Fields[vault.FieldPassword] = secretValue
	res, err = adm.SetCredential(smtp)
	if err != nil || res.State != control.StateApproved || !slices.Equal(res.Approved, []string{approval.ChannelsName}) {
		t.Fatalf("replace in a session = %+v, %v", res, err)
	}
}
