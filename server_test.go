// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// serve runs one request against a server built over the given store and returns
// the response recorder.
func serve(t *testing.T, s *Store, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	NewServer(s).ServeHTTP(w, r)
	return w
}

// decodeRows decodes a successful query response into rows.
func decodeRows(t *testing.T, w *httptest.ResponseRecorder) []Row {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var rows []Row
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode rows: %v (%s)", err, w.Body.String())
	}
	return rows
}

func TestServerQueryRootPQL(t *testing.T) {
	w := serve(t, fixture(), http.MethodPost, queryPath, `{"query":"nodes[certname]{ certname = \"web1\" }"}`)
	rows := decodeRows(t, w)
	if len(rows) != 1 || rows[0]["certname"] != "web1" {
		t.Fatalf("rows: %v", rows)
	}
}

func TestServerQueryRootAST(t *testing.T) {
	w := serve(t, fixture(), http.MethodPost, queryPath, `{"query":["from","nodes",["=","certname","db1"]]}`)
	rows := decodeRows(t, w)
	if len(rows) != 1 || rows[0]["certname"] != "db1" {
		t.Fatalf("rows: %v", rows)
	}
}

func TestServerQueryRootEmptyResultIsArray(t *testing.T) {
	w := serve(t, fixture(), http.MethodPost, queryPath, `{"query":"nodes{ certname = \"none\" }"}`)
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Fatalf("want [], got %q", got)
	}
}

func TestServerQueryEntityGET(t *testing.T) {
	// No query -> all rows.
	rows := decodeRows(t, serve(t, fixture(), http.MethodGet, queryPath+"/nodes", ""))
	if len(rows) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(rows))
	}
	// AST filter clause via query parameter.
	w := serve(t, fixture(), http.MethodGet, queryPath+`/nodes?query=%5B%22%3D%22%2C%22certname%22%2C%22web2%22%5D`, "")
	got := decodeRows(t, w)
	if len(got) != 1 || got[0]["certname"] != "web2" {
		t.Fatalf("filtered: %v", got)
	}
}

func TestServerQueryEntityPOST(t *testing.T) {
	// Bare filter clause.
	w := serve(t, fixture(), http.MethodPost, queryPath+"/resources", `{"query":["=","type","File"]}`)
	rows := decodeRows(t, w)
	if len(rows) != 2 {
		t.Fatalf("want 2 File resources, got %d", len(rows))
	}
	// Empty body -> all rows.
	all := decodeRows(t, serve(t, fixture(), http.MethodPost, queryPath+"/resources", ""))
	if len(all) != 3 {
		t.Fatalf("want 3 resources, got %d", len(all))
	}
	// Full from-node scoped to the entity path.
	fw := serve(t, fixture(), http.MethodPost, queryPath+"/nodes", `{"query":["from","nodes",["=","certname","web1"]]}`)
	if fr := decodeRows(t, fw); len(fr) != 1 || fr[0]["certname"] != "web1" {
		t.Fatalf("from-node: %v", fr)
	}
}

func TestServerCommandRoundTrip(t *testing.T) {
	s := NewStore()
	w := serve(t, s, http.MethodPost, commandPath+"?command=replace_facts&version=5&certname=n",
		`{"certname":"n","environment":"e","producer_timestamp":"t","values":{"a":"1"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp["uuid"]) != 36 {
		t.Fatalf("want a uuid, got %q", resp["uuid"])
	}
	rows, _ := s.Query(`facts{ certname = "n" }`)
	if len(rows) != 1 {
		t.Fatalf("command did not persist to store: %d rows", len(rows))
	}
}

func TestServerCommandPersistsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdb.json")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w := serve(t, db, http.MethodPost, commandPath+"?command=replace_facts&version=5&certname=n",
		`{"certname":"n","environment":"e","producer_timestamp":"t","values":{"a":"1"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := reopened.Query(`facts{ certname = "n" }`); len(rows) != 1 {
		t.Fatalf("command not persisted to disk: %d rows", len(rows))
	}
}

func TestServerErrors(t *testing.T) {
	cases := []struct {
		name   string
		method string
		target string
		body   string
		status int
	}{
		{"unknown path", http.MethodGet, "/pdb/other", "", http.StatusNotFound},
		{"root wrong method", http.MethodGet, queryPath, "", http.StatusMethodNotAllowed},
		{"root bad json", http.MethodPost, queryPath, `{`, http.StatusBadRequest},
		{"root query type", http.MethodPost, queryPath, `{"query":5}`, http.StatusBadRequest},
		{"root pql parse", http.MethodPost, queryPath, `{"query":"nodes{"}`, http.StatusBadRequest},
		{"root ast parse", http.MethodPost, queryPath, `{"query":["nope"]}`, http.StatusBadRequest},
		{"root eval error", http.MethodPost, queryPath, `{"query":"nodes{ certname ~ \"[\" }"}`, http.StatusBadRequest},
		{"entity unknown", http.MethodGet, queryPath + "/widgets", "", http.StatusNotFound},
		{"entity bad param", http.MethodGet, queryPath + "/nodes?query=%7B", "", http.StatusBadRequest},
		{"entity bad post", http.MethodPost, queryPath + "/nodes", `{`, http.StatusBadRequest},
		{"entity bad clause", http.MethodPost, queryPath + "/nodes", `{"query":["nope","a",1]}`, http.StatusBadRequest},
		{"entity wrong method", http.MethodPut, queryPath + "/nodes", "", http.StatusBadRequest},
		{"entity eval error", http.MethodPost, queryPath + "/nodes", `{"query":["~","certname","["]}`, http.StatusBadRequest},
		{"cmd wrong method", http.MethodGet, commandPath, "", http.StatusMethodNotAllowed},
		{"cmd missing command", http.MethodPost, commandPath + "?version=5", `{}`, http.StatusBadRequest},
		{"cmd bad version", http.MethodPost, commandPath + "?command=replace_facts&version=x", `{}`, http.StatusBadRequest},
		{"cmd ingest error", http.MethodPost, commandPath + "?command=replace_facts&version=5", `{`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(t, fixture(), tc.method, tc.target, tc.body)
			if w.Code != tc.status {
				t.Fatalf("status: got %d want %d (%s)", w.Code, tc.status, w.Body.String())
			}
		})
	}
}

// errReadCloser fails on Read, to exercise the payload-read error branch.
type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (errReadCloser) Close() error             { return nil }

func TestServerCommandReadError(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, commandPath+"?command=replace_facts&version=5", nil)
	r.Body = errReadCloser{}
	w := httptest.NewRecorder()
	NewServer(NewStore()).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 on read error, got %d", w.Code)
	}
}

func TestServerCommandSaveError(t *testing.T) {
	// Bind the store to a path whose parent directory does not exist, so the
	// post-ingest Save fails and the handler reports 500.
	db, err := Open(filepath.Join(t.TempDir(), "missing", "pdb.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := serve(t, db, http.MethodPost, commandPath+"?command=replace_facts&version=5&certname=n",
		`{"certname":"n","environment":"e","producer_timestamp":"t","values":{"a":"1"}}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 on save error, got %d: %s", w.Code, w.Body.String())
	}
}

// TestNewUUIDShape sanity-checks the default UUID generator.
func TestNewUUIDShape(t *testing.T) {
	u := newUUID()
	if len(u) != 36 || strings.Count(u, "-") != 4 || u[14] != '4' {
		t.Fatalf("malformed uuid %q", u)
	}
}

// io.Discard keeps the import used when the body is streamed verbatim.
var _ = io.Discard
