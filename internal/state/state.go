// Package state remembers recently shown photos so consecutive invocations do
// not repeat one another. It stores namespaced identifiers only.
package state

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/dotfrankruan/cattv/internal/fileutil"
)

// Version is the state file schema.
const Version = 1

const stateKind = "cattv-state"

const (
	// RemotePrefix namespaces remote photo ids in the history.
	RemotePrefix = "remote:"
	// LocalPrefix namespaces local library ids in the history.
	LocalPrefix = "local:"
)

// State is the persisted display history.
type State struct {
	Kind    string   `json:"kind"`
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
		return State{Kind: stateKind, Version: Version}, nil
	}
	data, err := fileutil.ReadFile(s.Path, true)
	if err != nil {
		return State{Kind: stateKind, Version: Version}, err
	}
	if len(data) == 0 {
		return State{Kind: stateKind, Version: Version}, nil
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{Kind: stateKind, Version: Version}, fmt.Errorf("parse state file: %w", err)
	}
	if st.Kind != "" && st.Kind != stateKind {
		return State{Kind: stateKind, Version: Version}, fmt.Errorf("file is %q, not CatTV display history", st.Kind)
	}
	if st.Version != Version {
		return State{Kind: stateKind, Version: Version}, fmt.Errorf("state schema %d is not supported", st.Version)
	}
	st.Kind = stateKind
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
		// Never overwrite an unreadable file: CATTV_STATE may be misconfigured
		// to point at another CatTV data file or user data. Viewing still
		// succeeds; only recency tracking is skipped for this request.
		return fmt.Errorf("refusing to overwrite unreadable display history: %w", err)
	}
	recent := make([]string, 0, s.limit())
	recent = append(recent, key)
	for _, old := range st.Recent {
		if old != key && len(recent) < s.limit() {
			recent = append(recent, old)
		}
	}
	st.Kind = stateKind
	st.Version = Version
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

// ExcludeIDs returns the bare ids for history entries with the given prefix.
// Catalog and library selectors operate on bare ids, so callers must not pass
// the namespaced history keys directly to them.
func (st State) ExcludeIDs(prefix string) map[string]bool {
	out := make(map[string]bool, len(st.Recent))
	for _, key := range st.Recent {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = true
		}
	}
	return out
}

// RemoteKey namespaces a remote photo id in the history.
func RemoteKey(id string) string { return RemotePrefix + id }

// LocalKey namespaces a local library id in the history.
func LocalKey(id string) string { return LocalPrefix + id }

func (s *Store) limit() int {
	if s.Limit <= 0 {
		return 32
	}
	return s.Limit
}
