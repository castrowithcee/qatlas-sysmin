package vault

import (
	"errors"
	"testing"
)

func TestReplaceKeepsOldIDReadableAndMovesName(t *testing.T) {
	v := newVault(t)
	oldID, err := v.Add("demo-registry", KindRegistry, registryFields())
	if err != nil {
		t.Fatal(err)
	}
	next := map[string]string{FieldUser: "synthetic-user", FieldToken: "synthetic-token-value-2222"}
	newID, err := v.Replace("demo-registry", next)
	if err != nil || newID == oldID {
		t.Fatalf("Replace() = %q, %v", newID, err)
	}
	byName, err := v.GetByName("demo-registry")
	if err != nil || byName.ID != newID {
		t.Fatalf("GetByName() = %+v, %v, want the new id", byName, err)
	}
	old, err := v.Get(oldID)
	if err != nil || old.Name != "demo-registry" {
		t.Fatalf("Get(old) = %+v, %v", old, err)
	}
	infos, err := v.List()
	if err != nil || len(infos) != 1 || infos[0].ID != newID {
		t.Fatalf("List() = %+v, %v, want only the new entry", infos, err)
	}
	if st, _ := v.Status(); st.Entries != 1 {
		t.Fatalf("Status().Entries = %d, want 1", st.Entries)
	}
	if err := v.Set(oldID, next); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Set(superseded) = %v, want ErrNotFound", err)
	}
	if err := v.Rename(oldID, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Rename(superseded) = %v, want ErrNotFound", err)
	}
}

func TestReplaceRefusals(t *testing.T) {
	v := newVault(t)
	if _, err := v.Replace("missing", registryFields()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Replace(missing) = %v", err)
	}
	if _, err := v.Add("demo-registry", KindRegistry, registryFields()); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Replace("demo-registry", map[string]string{FieldValue: "x"}); err == nil {
		t.Fatal("Replace() with the fields of another kind succeeded")
	}
	v.Forget()
	if _, err := v.Replace("demo-registry", registryFields()); !errors.Is(err, ErrLocked) {
		t.Fatalf("Replace() locked = %v, want ErrLocked", err)
	}
	if st, _ := v.Status(); st.Pending != 0 {
		t.Fatalf("a locked Replace queued %d pending entries", st.Pending)
	}
}

func TestReplaceSurvivesRelockAndPurge(t *testing.T) {
	v := newVault(t)
	oldID, _ := v.Add("demo-registry", KindRegistry, registryFields())
	midID, err := v.Replace("demo-registry", registryFields())
	if err != nil {
		t.Fatal(err)
	}
	newID, err := v.Replace("demo-registry", registryFields())
	if err != nil {
		t.Fatal(err)
	}
	v.Forget()
	if _, err := v.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(oldID); err != nil {
		t.Fatalf("Get(old) after unlock = %v", err)
	}
	n, err := v.PurgeSuperseded(map[string]bool{oldID: true})
	if err != nil || n != 1 {
		t.Fatalf("PurgeSuperseded() = %d, %v, want 1", n, err)
	}
	if _, err := v.Get(midID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(purged) = %v", err)
	}
	if _, err := v.Get(oldID); err != nil {
		t.Fatalf("Get(kept) = %v", err)
	}
	if e, err := v.GetByName("demo-registry"); err != nil || e.ID != newID {
		t.Fatalf("GetByName() = %+v, %v", e, err)
	}
	// A live entry is never purged, even when it is not in keep.
	if n, _ := v.PurgeSuperseded(nil); n != 1 {
		t.Fatalf("second purge removed %d, want only the kept superseded entry", n)
	}
	if _, err := v.GetByName("demo-registry"); err != nil {
		t.Fatal(err)
	}
}

func TestNameOfSupersededEntryCanBeReusedAfterDelete(t *testing.T) {
	v := newVault(t)
	if _, err := v.Add("demo-registry", KindRegistry, registryFields()); err != nil {
		t.Fatal(err)
	}
	newID, _ := v.Replace("demo-registry", registryFields())
	if _, err := v.Add("demo-registry", KindRegistry, registryFields()); !errors.Is(err, ErrExists) {
		t.Fatalf("Add(taken) = %v, want ErrExists", err)
	}
	if err := v.Delete(newID); err != nil {
		t.Fatal(err)
	}
	if _, err := v.GetByName("demo-registry"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a superseded entry resolves by name: %v", err)
	}
}
