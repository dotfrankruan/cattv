package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkSeenMaintainsBoundedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path, 2)
	if err := store.MarkSeen(RemoteKey("one")); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSeen(RemoteKey("two")); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSeen(RemoteKey("one")); err != nil {
		t.Fatal(err)
	}
	st, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Recent) != 2 || st.Recent[0] != RemoteKey("one") || st.Recent[1] != RemoteKey("two") {
		t.Fatalf("recent = %#v", st.Recent)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	excluded := st.ExcludeIDs(RemotePrefix)
	if !excluded["two"] || excluded["remote:two"] {
		t.Fatalf("exclude ids = %#v", excluded)
	}
	if local := st.ExcludeIDs(LocalPrefix); len(local) != 0 {
		t.Fatalf("local exclude ids = %#v", local)
	}
}

func TestMarkSeenRefusesToOverwriteCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path, 4)
	if err := store.MarkSeen(LocalKey("cat")); err == nil {
		t.Fatal("expected refusal to overwrite corrupt state")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{broken" {
		t.Fatalf("corrupt state was overwritten: %q", got)
	}
}

func TestStateRejectsOtherCatTVFileKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"kind":"cattv-library","version":1,"items":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path, 4).Load(); err == nil {
		t.Fatal("expected kind mismatch error")
	}
}

func TestLoadMissingState(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "none.json"), 4).Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != Version || st.Kind != stateKind || len(st.Recent) != 0 {
		t.Fatalf("state = %+v", st)
	}
}
