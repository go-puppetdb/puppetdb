// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotRoundTrip(t *testing.T) {
	s := fixture()
	data, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Load(data)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s2.Query(`nodes[certname]{} order by certname`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0]["certname"] != "db1" {
		t.Fatalf("loaded store query: %v", rows)
	}
}

func TestLoadInvalid(t *testing.T) {
	if _, err := Load([]byte(`not json`)); err == nil {
		t.Fatal("expected load error")
	}
}

func TestOpenSaveReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pdb.json")

	// Opening a missing file yields an empty store bound to the path.
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ingest(CmdReplaceFacts, 5, []byte(`{"certname":"n","environment":"e","producer_timestamp":"t","values":{"a":"1"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}

	// Reopening the same path restores the data.
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db2.Query(`facts{ certname = "n" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["value"] != "1" {
		t.Fatalf("reopened data: %v", rows)
	}
}

func TestSaveWithoutPath(t *testing.T) {
	if err := NewStore().Save(); err == nil {
		t.Fatal("expected error saving an unbound store")
	}
}

func TestOpenReadError(t *testing.T) {
	// A directory cannot be read as a file.
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("expected read error opening a directory")
	}
}

func TestOpenCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected load error for corrupt file")
	}
}

func TestSaveToBadDir(t *testing.T) {
	// A non-existent parent directory makes the temp-file write fail.
	err := fixture().SaveTo(filepath.Join(t.TempDir(), "nope", "pdb.json"))
	if err == nil {
		t.Fatal("expected save error for missing directory")
	}
}

func TestSaveToRenameError(t *testing.T) {
	// The destination is a directory, so the temp file writes but the rename
	// onto it fails.
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fixture().SaveTo(dest); err == nil {
		t.Fatal("expected rename error when destination is a directory")
	}
}

// unmarshalableStore builds a store holding a value that JSON cannot encode, so
// Snapshot (and thus SaveTo) fail.
func unmarshalableStore() *Store {
	s := NewStore()
	s.Add("nodes", Row{"bad": make(chan int)})
	return s
}

func TestSnapshotMarshalError(t *testing.T) {
	if _, err := unmarshalableStore().Snapshot(); err == nil {
		t.Fatal("expected snapshot marshal error")
	}
}

func TestSaveToSnapshotError(t *testing.T) {
	if err := unmarshalableStore().SaveTo(filepath.Join(t.TempDir(), "x.json")); err == nil {
		t.Fatal("expected save error from snapshot failure")
	}
}
