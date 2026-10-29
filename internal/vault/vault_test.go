package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
)

const (
	testPass   = "synthetic-passphrase-1"
	testToken  = "synthetic-token-value-7f3a"
	testSecret = "synthetic-secret-value-91bc"
)

// lowWorkFactor keeps every encrypted-vault test fast.
func lowWorkFactor(t *testing.T) {
	t.Helper()
	original := scryptLogN
	scryptLogN = 4
	t.Cleanup(func() { scryptLogN = original })
}

// newVault returns an unlocked vault created with testPass in a fresh directory.
func newVault(t *testing.T) *Vault {
	t.Helper()
	lowWorkFactor(t)
	v := New(filepath.Join(t.TempDir(), "vault"))
	if err := v.Create(testPass); err != nil {
		t.Fatalf("Create() = %v", err)
	}
	return v
}

func registryFields() map[string]string {
	return map[string]string{FieldUser: "synthetic-user", FieldToken: testToken}
}

func TestCreateWithoutPassphraseRefused(t *testing.T) {
	v := New(filepath.Join(t.TempDir(), "vault"))
	if err := v.Create(""); !errors.Is(err, ErrEmptyPassphrase) {
		t.Fatalf("Create(\"\") = %v, want ErrEmptyPassphrase", err)
	}
	if state, _ := v.State(); state != StateAbsent {
		t.Fatalf("State() = %v, want absent", state)
	}
	if _, err := os.Stat(v.Dir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused Create left %s behind: %v", v.Dir(), err)
	}
}

func TestCreateAndOpenWithPassphrase(t *testing.T) {
	v := newVault(t)
	if err := v.Create(testPass); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create() = %v, want ErrExists", err)
	}
	id, err := v.Add("reg", KindRegistry, registryFields())
	if err != nil {
		t.Fatal(err)
	}

	again := New(v.Dir())
	if state, _ := again.State(); state != StateLocked {
		t.Fatalf("State() = %v, want locked", state)
	}
	if _, err := again.Get(id); !errors.Is(err, ErrLocked) {
		t.Fatalf("Get() of a locked vault = %v, want ErrLocked", err)
	}
	if _, err := again.Unlock("wrong-passphrase"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("Unlock(wrong) = %v, want ErrWrongPassphrase", err)
	}
	if state, _ := again.State(); state != StateLocked {
		t.Fatalf("a wrong passphrase unlocked the vault")
	}
	if _, err := again.Unlock(testPass); err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
	e, err := again.Get(id)
	if err != nil || e.Fields[FieldToken].Reveal() != testToken {
		t.Fatalf("Get() = %+v, %v", e, err)
	}
}

func TestUnlockAbsentVault(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "vault")).Unlock(testPass); !errors.Is(err, ErrAbsent) {
		t.Fatalf("Unlock() of an absent vault = %v, want ErrAbsent", err)
	}
}

func TestChangePassphrase(t *testing.T) {
	v := newVault(t)
	id, _ := v.Add("reg", KindRegistry, registryFields())
	keyBefore := mustIdentity(t, v)
	recipientBefore, _ := v.Recipient()
	logBefore := mustLogKey(t, v)

	if err := v.ChangePassphrase("wrong-old", "new-passphrase"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("ChangePassphrase(wrong old) = %v", err)
	}
	if err := v.ChangePassphrase(testPass, ""); !errors.Is(err, ErrEmptyPassphrase) {
		t.Fatalf("ChangePassphrase(empty new) = %v", err)
	}
	if err := v.ChangePassphrase(testPass, "new-passphrase"); err != nil {
		t.Fatalf("ChangePassphrase() = %v", err)
	}

	locked := New(v.Dir())
	if _, err := locked.Unlock(testPass); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("Unlock(old) after the change = %v, want ErrWrongPassphrase", err)
	}
	if _, err := locked.Unlock("new-passphrase"); err != nil {
		t.Fatalf("Unlock(new) = %v", err)
	}
	if got := mustIdentity(t, locked).String(); got != keyBefore.String() {
		t.Fatal("the vault key changed with the passphrase")
	}
	if got, _ := locked.Recipient(); got != recipientBefore {
		t.Fatal("the recipient changed with the passphrase")
	}
	if !sameLogKey(t, logBefore, mustLogKey(t, locked)) {
		t.Fatal("the log key changed with the passphrase")
	}
	if _, err := locked.Get(id); err != nil {
		t.Fatalf("an entry is gone after the change: %v", err)
	}
}

func TestVerifyPassphrase(t *testing.T) {
	v := newVault(t)
	if err := v.VerifyPassphrase(testPass); err != nil {
		t.Fatal(err)
	}
	if err := v.VerifyPassphrase("nope"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("VerifyPassphrase(wrong) = %v", err)
	}
	if err := New(filepath.Join(t.TempDir(), "x")).VerifyPassphrase(testPass); !errors.Is(err, ErrAbsent) {
		t.Fatalf("VerifyPassphrase() of an absent vault = %v", err)
	}
}

func TestEntryLifecycleForEveryKind(t *testing.T) {
	v := newVault(t)
	cases := map[Kind]map[string]string{
		KindRegistry: {FieldUser: "u", FieldToken: testToken},
		KindGitHTTPS: {FieldUser: "u", FieldToken: testToken},
		KindSSHKey:   {FieldPrivateKey: "synthetic-private-key", FieldHostKey: "example.invalid ssh-ed25519 AAAAsynthetic"},
		KindSMTP:     {FieldUser: "u", FieldPassword: testSecret},
		KindSecret:   {FieldValue: testSecret},
	}
	if len(cases) != len(Kinds()) {
		t.Fatalf("test covers %d kinds, package has %d", len(cases), len(Kinds()))
	}
	for kind, fields := range cases {
		id, err := v.Add("entry-"+string(kind), kind, fields)
		if err != nil {
			t.Fatalf("Add(%s) = %v", kind, err)
		}
		e, err := v.Get(id)
		if err != nil || e.Kind != kind || len(e.Fields) != len(fields) {
			t.Fatalf("Get(%s) = %+v, %v", kind, e, err)
		}
		for name, want := range fields {
			if e.Fields[name].Reveal() != want {
				t.Fatalf("%s field %s differs", kind, name)
			}
		}
	}

	infos, err := v.List()
	if err != nil || len(infos) != len(cases) {
		t.Fatalf("List() = %v, %v", infos, err)
	}

	// Set replaces the values; Delete removes the entry.
	e, _ := v.GetByName("entry-secret")
	if err := v.Set(e.ID, map[string]string{FieldValue: "synthetic-second-value"}); err != nil {
		t.Fatal(err)
	}
	got, _ := v.Get(e.ID)
	if got.Fields[FieldValue].Reveal() != "synthetic-second-value" || got.ID != e.ID {
		t.Fatalf("Set() did not replace the value: %+v", got)
	}
	if err := v.Delete(e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Delete() = %v", err)
	}
	if err := v.Delete(e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete() = %v", err)
	}

	// The change survives a reopen.
	again := New(v.Dir())
	if _, err := again.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if _, err := again.Get(e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deleted entry came back: %v", err)
	}
	if _, err := again.GetByName("entry-smtp"); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidEntriesRefused(t *testing.T) {
	v := newVault(t)
	for name, tc := range map[string]struct {
		name   string
		kind   Kind
		fields map[string]string
	}{
		"empty name":    {"", KindSecret, map[string]string{FieldValue: "x"}},
		"control name":  {"a\nb", KindSecret, map[string]string{FieldValue: "x"}},
		"unknown kind":  {"n", Kind("nope"), map[string]string{FieldValue: "x"}},
		"missing field": {"n", KindRegistry, map[string]string{FieldUser: "u"}},
		"extra field":   {"n", KindSecret, map[string]string{FieldValue: "x", FieldUser: "u"}},
		"empty value":   {"n", KindSecret, map[string]string{FieldValue: ""}},
	} {
		if _, err := v.Add(tc.name, tc.kind, tc.fields); err == nil {
			t.Errorf("%s: Add() accepted the entry", name)
		}
	}
	if _, err := v.Add("dup", KindSecret, map[string]string{FieldValue: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Add("dup", KindSecret, map[string]string{FieldValue: "y"}); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate name = %v, want ErrExists", err)
	}
}

func TestRenameKeepsID(t *testing.T) {
	v := newVault(t)
	id, _ := v.Add("old-name", KindGitHTTPS, registryFields())
	other, _ := v.Add("taken", KindSecret, map[string]string{FieldValue: "x"})
	_ = other
	if err := v.Rename(id, "taken"); !errors.Is(err, ErrExists) {
		t.Fatalf("Rename() onto a taken name = %v", err)
	}
	if err := v.Rename(id, "new-name"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.GetByName("old-name"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old name still resolves: %v", err)
	}
	e, err := v.Get(id)
	if err != nil || e.Name != "new-name" || e.ID != id {
		t.Fatalf("Get() after Rename() = %+v, %v", e, err)
	}
	if err := v.Rename("0123456789abcdef0123456789abcdef", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Rename() of an unknown id = %v", err)
	}

	again := New(v.Dir())
	if _, err := again.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if e, err := again.GetByName("new-name"); err != nil || e.ID != id {
		t.Fatalf("after reopen: %+v, %v", e, err)
	}
}

func TestPendingAddNeedsNoPassphraseAndMergesOnUnlock(t *testing.T) {
	v := newVault(t)
	locked := New(v.Dir())

	id, err := locked.Add("late", KindSMTP, map[string]string{FieldUser: "u", FieldPassword: testSecret})
	if err != nil {
		t.Fatalf("Add() on a locked vault = %v", err)
	}
	if _, err := locked.Get(id); !errors.Is(err, ErrLocked) {
		t.Fatalf("Get() of a pending entry = %v, want ErrLocked", err)
	}
	st, _ := locked.Status()
	if st.Pending != 1 || st.Entries != -1 {
		t.Fatalf("Status() = %+v", st)
	}

	// The pending file holds no plaintext.
	files, _ := os.ReadDir(filepath.Join(v.Dir(), pendingDir))
	for _, f := range files {
		data, _ := os.ReadFile(filepath.Join(v.Dir(), pendingDir, f.Name()))
		if bytes.Contains(data, []byte(testSecret)) {
			t.Fatal("a pending file holds the secret in plaintext")
		}
	}

	// A vault with the key but not unlocked by passphrase leaves it pending.
	withKey, err := OpenWithKey(v.Dir(), mustIdentity(t, v))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withKey.Get(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OpenWithKey() merged a pending entry: %v", err)
	}
	if st, _ := withKey.Status(); st.Pending != 1 {
		t.Fatalf("OpenWithKey() removed the pending entry: %+v", st)
	}

	merged, err := locked.Unlock(testPass)
	if err != nil || merged != 1 {
		t.Fatalf("Unlock() = %d, %v, want 1 merged", merged, err)
	}
	e, err := locked.Get(id)
	if err != nil || e.Name != "late" || e.Fields[FieldPassword].Reveal() != testSecret {
		t.Fatalf("Get() after the merge = %+v, %v", e, err)
	}
	if st, _ := locked.Status(); st.Pending != 0 || st.Entries != 1 {
		t.Fatalf("Status() after the merge = %+v", st)
	}
}

func TestPendingAddOnAbsentVaultRefused(t *testing.T) {
	v := New(filepath.Join(t.TempDir(), "vault"))
	if _, err := v.Add("n", KindSecret, map[string]string{FieldValue: "x"}); !errors.Is(err, ErrAbsent) {
		t.Fatalf("Add() on an absent vault = %v", err)
	}
}

func TestPendingNameConflictStaysPending(t *testing.T) {
	v := newVault(t)
	locked := New(v.Dir())
	if _, err := locked.Add("same", KindSecret, map[string]string{FieldValue: "from-pending"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Add("same", KindSecret, map[string]string{FieldValue: "from-unlocked"}); err != nil {
		t.Fatal(err)
	}
	merged, err := locked.Unlock(testPass)
	if err != nil || merged != 0 {
		t.Fatalf("Unlock() = %d, %v, want 0 merged", merged, err)
	}
	if st, _ := locked.Status(); st.Pending != 1 {
		t.Fatalf("the conflicting entry did not stay pending: %+v", st)
	}
	if e, _ := locked.GetByName("same"); e.Fields[FieldValue].Reveal() != "from-unlocked" {
		t.Fatal("a pending entry replaced an existing one")
	}
}

func TestForgetLocksAgain(t *testing.T) {
	v := newVault(t)
	id, _ := v.Add("n", KindSecret, map[string]string{FieldValue: "x"})
	v.Forget()
	if state, _ := v.State(); state != StateLocked {
		t.Fatalf("State() after Forget() = %v", state)
	}
	if _, err := v.Get(id); !errors.Is(err, ErrLocked) {
		t.Fatalf("Get() after Forget() = %v", err)
	}
}

func TestOpenWithKey(t *testing.T) {
	v := newVault(t)
	id, _ := v.Add("n", KindSecret, map[string]string{FieldValue: testSecret})

	opened, err := OpenWithKey(v.Dir(), mustIdentity(t, v))
	if err != nil {
		t.Fatal(err)
	}
	if e, err := opened.Get(id); err != nil || e.Fields[FieldValue].Reveal() != testSecret {
		t.Fatalf("Get() = %+v, %v", e, err)
	}
	other := newVault(t)
	if _, err := OpenWithKey(v.Dir(), mustIdentity(t, other)); err == nil {
		t.Fatal("OpenWithKey() accepted a key that does not belong to the vault")
	}
	if _, err := OpenWithKey(filepath.Join(t.TempDir(), "none"), mustIdentity(t, v)); !errors.Is(err, ErrAbsent) {
		t.Fatalf("OpenWithKey() of an absent vault = %v", err)
	}
	if _, err := OpenWithKey(v.Dir(), nil); err == nil {
		t.Fatal("OpenWithKey(nil) succeeded")
	}
}

func TestDirAndFileModes(t *testing.T) {
	v := newVault(t)
	if _, err := New(v.Dir()).Add("p", KindSecret, map[string]string{FieldValue: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Add("q", KindSecret, map[string]string{FieldValue: "y"}); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(v.Dir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		want := fs.FileMode(fileMode)
		if d.IsDir() {
			want = dirMode
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := filepath.Glob(filepath.Join(v.Dir(), ".sysmin-vault-*")); len(entries) != 0 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestWidenedFileIsRefused(t *testing.T) {
	v := newVault(t)
	if err := os.Chmod(filepath.Join(v.Dir(), secretsFile), 0o644); err != nil {
		t.Fatal(err)
	}
	var perm *PermissionError
	if _, err := New(v.Dir()).Unlock(testPass); !errors.As(err, &perm) {
		t.Fatalf("Unlock() with a widened file = %v, want *PermissionError", err)
	}
}

func TestUnsupportedSchemaVersionIsRefused(t *testing.T) {
	v := newVault(t)
	data, err := encryptTo(`{"schema":2,"entries":{}}`, v.identity.recipient)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v.Dir(), secretsFile), data, fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := New(v.Dir()).Unlock(testPass); err == nil ||
		!strings.Contains(err.Error(), "unsupported vault schema version 2") {
		t.Fatalf("Unlock() of a future schema = %v", err)
	}
}

func TestFailedWriteKeepsMemoryUnchanged(t *testing.T) {
	v := newVault(t)
	if _, err := v.Add("keep", KindSecret, map[string]string{FieldValue: "x"}); err != nil {
		t.Fatal(err)
	}
	// Make the replacement of secrets.age impossible: a directory cannot be renamed over by a file.
	if err := os.Remove(filepath.Join(v.Dir(), secretsFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(v.Dir(), secretsFile), dirMode); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Add("lost", KindSecret, map[string]string{FieldValue: "y"}); err == nil {
		t.Fatal("Add() succeeded although the write failed")
	}
	if _, err := v.GetByName("lost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a failed write changed the in-memory document: %v", err)
	}
}

func TestStatus(t *testing.T) {
	absent := New(filepath.Join(t.TempDir(), "vault"))
	if st, err := absent.Status(); err != nil || st.State != StateAbsent || st.Entries != 0 || st.Pending != 0 {
		t.Fatalf("Status() of an absent vault = %+v, %v", st, err)
	}
	v := newVault(t)
	_, _ = v.Add("n", KindSecret, map[string]string{FieldValue: "x"})
	if st, _ := v.Status(); st.State != StateUnlocked || st.Entries != 1 || st.Dir != v.Dir() {
		t.Fatalf("Status() = %+v", st)
	}
	if st, _ := New(v.Dir()).Status(); st.State != StateLocked || st.Entries != -1 {
		t.Fatalf("Status() of a locked vault = %+v", st)
	}
}

func TestLogKey(t *testing.T) {
	v := newVault(t)
	if _, err := New(v.Dir()).LogKey(); !errors.Is(err, ErrLocked) {
		t.Fatalf("LogKey() of a locked vault = %v, want ErrLocked", err)
	}
	first := mustLogKey(t, v)
	again := New(v.Dir())
	if _, err := again.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if !sameLogKey(t, first, mustLogKey(t, again)) {
		t.Fatal("the same vault yielded different log keys")
	}
	withKey, _ := OpenWithKey(v.Dir(), mustIdentity(t, v))
	if !sameLogKey(t, first, mustLogKey(t, withKey)) {
		t.Fatal("opening with the key yielded a different log key")
	}
	if sameLogKey(t, first, mustLogKey(t, newVault(t))) {
		t.Fatal("two vaults yielded the same log key")
	}
}

// The log key signs an audit entry that the same vault's key verifies as valid, and another vault's does not.
func TestLogKeySignsAuditEntries(t *testing.T) {
	v := newVault(t)
	dir := t.TempDir()
	logger := auditlog.New(dir, 90).WithKey(mustLogKey(t, v))
	if !logger.Signs() {
		t.Fatal("the logger does not sign with the vault's log key")
	}
	err := logger.Append(auditlog.Fields{ClientUID: 1000, Action: auditlog.OperationConfigApply,
		Project: "demo", Result: auditlog.ResultSuccess})
	if err != nil {
		t.Fatal(err)
	}
	reopened := New(v.Dir())
	if _, err := reopened.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if state := signatureState(t, dir, mustLogKey(t, reopened)); state != auditlog.SignatureValid {
		t.Fatalf("signature state with the same vault's key = %q, want valid", state)
	}
	if state := signatureState(t, dir, mustLogKey(t, newVault(t))); state != auditlog.SignatureInvalid {
		t.Fatalf("signature state with another vault's key = %q, want invalid", state)
	}
}

func TestSecretsRedactThemselves(t *testing.T) {
	v := newVault(t)
	id, _ := v.Add("reg", KindRegistry, registryFields())
	e, _ := v.Get(id)
	list, _ := v.List()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("entry", "entry", e, "token", e.Fields[FieldToken])
	jsonBytes, _ := json.Marshal(e)
	jsonOut := string(jsonBytes)

	outputs := []string{
		fmt.Sprintf("%v %+v %#v %s %q %x", e, e, e, e, e, e),
		fmt.Sprintf("%v %+v %#v", e.Fields, e.Fields, e.Fields),
		fmt.Sprint(e.Fields[FieldToken]), fmt.Sprintf("%#v", e.Fields[FieldToken]),
		fmt.Sprintf("%v", list), buf.String(), jsonOut,
	}
	for i, out := range outputs {
		if strings.Contains(out, testToken) {
			t.Errorf("output %d contains the secret value: %s", i, out)
		}
	}
	if !strings.Contains(outputs[2], redacted) {
		t.Errorf("a Secret prints as %q, want %q", outputs[2], redacted)
	}
	if e.Fields[FieldToken].Reveal() != testToken {
		t.Fatal("Reveal() lost the value")
	}
}

func TestErrorsNeverContainSecretValues(t *testing.T) {
	v := newVault(t)
	bad := []struct {
		name   string
		kind   Kind
		fields map[string]string
	}{
		{"n", KindRegistry, map[string]string{FieldUser: "u", FieldToken: testToken, "extra": testSecret}},
		{"n", KindRegistry, map[string]string{FieldToken: testToken}},
		{"n", Kind(testSecret), map[string]string{FieldValue: testSecret}},
		{"n", KindSecret, map[string]string{FieldValue: testSecret, FieldUser: testToken}},
	}
	for _, tc := range bad {
		_, err := v.Add(tc.name, tc.kind, tc.fields)
		if err == nil {
			t.Errorf("Add(%v) succeeded", tc.fields)
			continue
		}
		// The unknown-kind message names the kind the caller chose, which is a label, not a value; the
		// secrets in the fields must never be quoted.
		if strings.Contains(err.Error(), testToken) {
			t.Errorf("error quotes a field value: %v", err)
		}
	}
	id, _ := v.Add("ok", KindSecret, map[string]string{FieldValue: testSecret})
	for _, err := range []error{
		v.Set(id, map[string]string{FieldValue: testSecret, FieldToken: testToken}),
		v.Set(id, map[string]string{}),
		v.Rename(id, ""),
		v.ChangePassphrase(testSecret, testToken+"-new"),
		v.VerifyPassphrase(testToken),
	} {
		if err != nil && (strings.Contains(err.Error(), testSecret) || strings.Contains(err.Error(), testToken)) {
			t.Errorf("error contains a secret: %v", err)
		}
	}
	_, err := New(v.Dir()).Unlock(testToken)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Errorf("Unlock(wrong) error = %v", err)
	}
}

func TestCheckpointRestores(t *testing.T) {
	v := newVault(t)
	id, _ := v.Add("n", KindSecret, map[string]string{FieldValue: "before"})
	restore, err := v.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Set(id, map[string]string{FieldValue: "after"}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(v.Dir()).Add("pend", KindSecret, map[string]string{FieldValue: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if e, _ := v.Get(id); e.Fields[FieldValue].Reveal() != "before" {
		t.Fatal("Checkpoint restore did not bring the value back in memory")
	}
	if st, _ := v.Status(); st.Pending != 0 {
		t.Fatalf("Checkpoint restore left a pending entry: %+v", st)
	}
	again := New(v.Dir())
	if _, err := again.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if e, _ := again.Get(id); e.Fields[FieldValue].Reveal() != "before" {
		t.Fatal("Checkpoint restore did not bring the file back")
	}
}

func TestCheckpointOfAbsentVaultRemovesIt(t *testing.T) {
	lowWorkFactor(t)
	v := New(filepath.Join(t.TempDir(), "vault"))
	restore, err := v.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Create(testPass); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v.Dir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("restore left the vault directory: %v", err)
	}
	if state, _ := v.State(); state != StateAbsent {
		t.Fatalf("State() = %v", state)
	}
}

// A real vault key keeps age's own work factor; only a test lowers it, and restores it afterwards.
func TestProductionWorkFactorUnchanged(t *testing.T) {
	if scryptLogN != 18 {
		t.Fatalf("scryptLogN = %d, want 18", scryptLogN)
	}
}

func TestReadPassphraseFrom(t *testing.T) {
	got, err := ReadPassphraseFrom(strings.NewReader("synthetic-pass\r\nrest"))
	if err != nil || got != "synthetic-pass" {
		t.Fatalf("ReadPassphraseFrom() = %q, %v", got, err)
	}
	if _, err := ReadPassphraseFrom(strings.NewReader("")); !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("ReadPassphraseFrom(empty) = %v", err)
	}
}

func TestUnwrapIdentityHasNoSideEffects(t *testing.T) {
	v := newVault(t)
	locked := New(v.Dir())
	if _, err := locked.Add("later", KindSecret, map[string]string{FieldValue: testSecret}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(v.Dir(), secretsFile))
	pending, _ := os.ReadDir(filepath.Join(v.Dir(), pendingDir))
	if len(pending) != 1 {
		t.Fatalf("pending = %d", len(pending))
	}
	id, err := locked.UnwrapIdentity(testPass)
	if err != nil || id == nil {
		t.Fatalf("UnwrapIdentity() = %v, %v", id, err)
	}
	if st, _ := locked.State(); st != StateLocked {
		t.Errorf("state = %s", st)
	}
	after, _ := os.ReadFile(filepath.Join(v.Dir(), secretsFile))
	if !bytes.Equal(before, after) {
		t.Error("secrets.age changed")
	}
	if p, _ := os.ReadDir(filepath.Join(v.Dir(), pendingDir)); len(p) != 1 {
		t.Error("pending changed")
	}
	if _, err := locked.UnwrapIdentity("wrong"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("wrong passphrase err = %v", err)
	}
	if _, err := New(t.TempDir()).UnwrapIdentity(testPass); !errors.Is(err, ErrAbsent) {
		t.Errorf("absent err = %v", err)
	}
}
