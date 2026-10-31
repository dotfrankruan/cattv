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
	if !st.Exclude()[RemoteKey("two")] {
		t.Fatal("exclude map missing key")
	}
}

func TestMarkSeenRecoversFromCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path, 4)
	if err := store.MarkSeen(LocalKey("cat")); err != nil {
		t.Fatal(err)
	}
	st, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Recent) != 1 || st.Recent[0] != LocalKey("cat") {
		t.Fatalf("recent = %#v", st.Recent)
	}
}

func TestLoadMissingState(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "none.json"), 4).Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != Version || len(st.Recent) != 0 {
		t.Fatalf("state = %+v", st)
	}
}
