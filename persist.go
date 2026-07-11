// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"encoding/json"
	"fmt"
	"os"
)

// Snapshot serialises the whole store to JSON: an object mapping each entity
// name to its array of rows. The encoding round-trips through [Load].
func (s *Store) Snapshot() ([]byte, error) {
	b, err := json.Marshal(s.entities)
	if err != nil {
		return nil, fmt.Errorf("puppetdb: snapshot: %w", err)
	}
	return b, nil
}

// Load reconstructs a store from a [Store.Snapshot] JSON document.
func Load(data []byte) (*Store, error) {
	ents := map[string][]Row{}
	if err := json.Unmarshal(data, &ents); err != nil {
		return nil, fmt.Errorf("puppetdb: load: %w", err)
	}
	return &Store{entities: ents}, nil
}

// Open returns a store backed by the JSON file at path — a pure-Go embedded
// storage backend that needs no external database. When the file does not yet
// exist an empty store bound to path is returned, so a fresh server can start
// and later [Store.Save] to it.
func Open(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s := NewStore()
			s.path = path
			return s, nil
		}
		return nil, fmt.Errorf("puppetdb: open %s: %w", path, err)
	}
	s, err := Load(data)
	if err != nil {
		return nil, err
	}
	s.path = path
	return s, nil
}

// Save atomically persists the store to its bound path (set by [Open]), writing
// to a temporary file in the same directory and renaming it into place.
func (s *Store) Save() error {
	if s.path == "" {
		return fmt.Errorf("puppetdb: save: store has no bound path (use Open)")
	}
	return s.SaveTo(s.path)
}

// SaveTo atomically persists the store to path: it writes a sibling temporary
// file and renames it into place, so a crash mid-write never truncates an
// existing snapshot.
func (s *Store) SaveTo(path string) error {
	data, err := s.Snapshot()
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("puppetdb: save: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("puppetdb: save: rename: %w", err)
	}
	return nil
}
