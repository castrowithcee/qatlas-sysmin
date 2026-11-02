package cli

import (
	"os"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

func folderFixture(t *testing.T) *changeFixture {
	t.Helper()
	f := newChangeFixture(t)
	if err := hostchange.RegisterFolderActions(f.env.Registry, hostchange.FolderHost{Accounts: hostadmin.NewHost(false)}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestHostApplyRefusesASystemPathWithoutTheAcknowledgement(t *testing.T) {
	f := folderFixture(t)
	args := hostchange.FolderArgs(hostchange.FolderRequest{
		Path: "/etc/demo-app", Owner: "demo-user", Group: "demo-group",
		UserAccess: hostadmin.AccessReadWrite, GroupAccess: hostadmin.AccessRead, OtherAccess: hostadmin.AccessNothing,
	})
	cmd := append([]string{"host", "apply", "--yes", "--output", "json", "--", hostchange.ActionFolderSetPermissions}, args...)
	code, out, errOut := runChange(t, f, 0, "", cmd...)
	if code != 1 {
		t.Fatalf("code %d err %q", code, errOut)
	}
	m := decode(t, out)
	if m["result"] != "refused" || m["action"] != hostchange.ActionFolderSetPermissions || m["journal_id"] != "" {
		t.Errorf("%v", m)
	}
	if len(f.w.calls) != 0 {
		t.Errorf("executed %v", f.w.calls)
	}
	if files, _ := os.ReadDir(f.dir); len(files) != 0 {
		t.Errorf("a refusal wrote %d journal files", len(files))
	}
}
