package notify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// approvalEnv is a real approval manager over a temporary configuration and vault.
type approvalEnv struct {
	t     *testing.T
	root  string
	vault *vault.Vault
	mgr   *approval.Manager
	ids   map[string]string
}

func newApprovalEnv(t *testing.T) *approvalEnv {
	t.Helper()
	vault.LowerWorkFactorForTests()
	e := &approvalEnv{t: t, root: t.TempDir(), ids: map[string]string{}}
	e.vault = vault.New(filepath.Join(t.TempDir(), "vault"))
	if err := e.vault.Create("demo-passphrase-1"); err != nil {
		t.Fatal(err)
	}
	e.mgr = approval.New(config.NewStore(e.root), e.vault, filepath.Join(t.TempDir(), "state"))
	return e
}

func (e *approvalEnv) writeSettings(body string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.root, "sysmin.yaml"), []byte("schema: 1\n"+body), 0o640); err != nil {
		e.t.Fatal(err)
	}
}

func (e *approvalEnv) addSecret(name, value string) {
	e.t.Helper()
	id, err := e.vault.Add(name, vault.KindSecret, map[string]string{vault.FieldValue: value})
	if err != nil {
		e.t.Fatal(err)
	}
	e.ids[name] = id
}
