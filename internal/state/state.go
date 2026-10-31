// Package state remembers recently shown photos so consecutive invocations do
// not repeat one another. It stores identifiers and timestamps only.
package state

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/dotfrankruan/cattv/internal/fileutil"
)

// Version is the state file schema.
const Version = 1

// State is the persisted display history.
type State struct {
	Version int      `json:"version"`
	Recent  []string `json:"recent,omitempty"`
}

// Store reads and writes a bounded recent-photo history.
type Store struct {
	Path  string
	Limit int
}

// NewStore constructs a store. Limit defaults to 32.
func NewStore(path string, limit int) *Store {
	if limit <= 0 {
		limit = 32
	}
	return &Store{Path: path, Limit: limit}
}

// Load returns an empty state when no file exists. Corrupt state is reported
// but never blocks viewing.
func (s *Store) Load() (State, error) {
	if s == nil || s.Path == "" {
		return State{Version: Version}, nil
	}
	data, err := fileutil.ReadFile(s.Path, true)
	if err != nil {
		return State{Version: Version}, err
	}
	if len(data) == 0 {
		return State{Version: Version}, nil
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{Version: Version}, fmt.Errorf("parse state file: %w", err)
	}
	if st.Version != Version {
		return State{Version: Version}, fmt.Errorf("state schema %d is not supported", st.Version)
	}
	if len(st.Recent) > s.limit() {
		st.Recent = st.Recent[:s.limit()]
	}
	return st, nil
}

// MarkSeen records key as most recent and atomically persists the history.
func (s *Store) MarkSeen(key string) error {
	if s == nil || s.Path == "" || key == "" {
		return nil
	}
	st, err := s.Load()
	if err != nil {
		// A damaged history should not prevent showing a cat; start fresh.
		st = State{Version: Version}
	}
	recent := make([]string, 0, s.limit())
	recent = append(recent, key)
	for _, old := range st.Recent {
		if old != key && len(recent) < s.limit() {
			recent = append(recent, old)
		}
	}
	st.Recent = recent
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.WriteAtomic(s.Path, 0o600, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// Exclude converts recent keys into a lookup map.
func (st State) Exclude() map[string]bool {
	out := make(map[string]bool, len(st.Recent))
	for _, key := range st.Recent {
		out[key] = true
	}
	return out
}

// RemoteKey namespaces a remote photo id in the history.
func RemoteKey(id string) string { return "remote:" + id }

// LocalKey namespaces a local library id in the history.
func LocalKey(id string) string { return "local:" + id }

func (s *Store) limit() int {
	if s.Limit <= 0 {
		return 32
	}
	return s.Limit
}

// Remove deletes the state file, primarily for tests and explicit resets.
func (s *Store) Remove() error {
	if s == nil || s.Path == "" {
		return nil
	}
	err := os.Remove(s.Path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return err
}
