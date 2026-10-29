package vault

import (
	"testing"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
)

func mustIdentity(t *testing.T, v *Vault) *age.X25519Identity {
	t.Helper()
	id, err := v.Identity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustLogKey(t *testing.T, v *Vault) *auditlog.Key {
	t.Helper()
	key, err := v.LogKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// signatureState signs one entry with signer in a fresh log and returns how checker judges that log.
func signatureStateOf(t *testing.T, signer, checker *auditlog.Key) string {
	t.Helper()
	dir := t.TempDir()
	err := auditlog.New(dir, 90).WithKey(signer).Append(auditlog.Fields{ClientUID: 1,
		Action: auditlog.OperationConfigApply, Result: auditlog.ResultSuccess})
	if err != nil {
		t.Fatal(err)
	}
	return signatureState(t, dir, checker)
}

func signatureState(t *testing.T, dir string, checker *auditlog.Key) string {
	t.Helper()
	report, err := auditlog.Verify(dir, checker)
	if err != nil || len(report.Days) != 1 {
		t.Fatalf("Verify() = %+v, %v", report, err)
	}
	return report.Days[0].SignatureState(report.Checked)
}

// sameLogKey reports whether a and b are the same key: an entry signed with one is valid for the other.
func sameLogKey(t *testing.T, a, b *auditlog.Key) bool {
	t.Helper()
	return signatureStateOf(t, a, b) == auditlog.SignatureValid
}
