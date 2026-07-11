// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Server is a pure-Go PuppetDB HTTP endpoint backed by a [Store]. It serves the
// query API at /pdb/query/v4 (both the root PQL/AST endpoint and the per-entity
// paths) and ingests commands at /pdb/cmd/v1. It implements [net/http.Handler].
//
// The store is guarded by a read/write mutex, so queries and command ingestion
// are safe to serve concurrently. When the store was created with [Open], each
// successful command is persisted with [Store.Save] before the response is sent.
type Server struct {
	mu     sync.RWMutex
	store  *Store
	genUID func() string // overridable for tests; defaults to a v4 UUID
}

// NewServer returns a server serving queries and commands against store.
func NewServer(store *Store) *Server {
	return &Server{store: store, genUID: newUUID}
}

// ServeHTTP routes a request to the query or command handlers.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == queryPath:
		s.handleQueryRoot(w, r)
	case strings.HasPrefix(r.URL.Path, queryPath+"/"):
		s.handleQueryEntity(w, r, strings.TrimPrefix(r.URL.Path, queryPath+"/"))
	case r.URL.Path == commandPath:
		s.handleCommand(w, r)
	default:
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown path %q", r.URL.Path))
	}
}

// commandPath is the PuppetDB command-ingest endpoint.
const commandPath = "/pdb/cmd/v1"

// handleQueryRoot serves POST /pdb/query/v4, accepting a PQL string or an AST
// array in the request body's "query" field.
func (s *Server) handleQueryRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "query endpoint requires POST")
		return
	}
	var body struct {
		Query any `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	q, err := queryFromRequest(body.Query)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.runQuery(w, q)
}

// queryFromRequest builds a query from a decoded "query" field that is either a
// PQL string or an AST ["from", ...] array.
func queryFromRequest(v any) (*Query, error) {
	switch q := v.(type) {
	case string:
		return Parse(q)
	case []any:
		return queryFromAST(q)
	default:
		return nil, fmt.Errorf("query must be a PQL string or an AST array")
	}
}

// handleQueryEntity serves the per-entity paths /pdb/query/v4/<entity>. The
// optional query (GET "query" parameter or POST body "query" field) is an AST
// clause scoped to the entity: a bare filter, an ["extract", ...] node, or a
// full ["from", ...] query.
func (s *Server) handleQueryEntity(w http.ResponseWriter, r *http.Request, entity string) {
	if !entities[entity] {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown entity %q", entity))
		return
	}
	raw, err := entityQueryRaw(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	q, err := queryForEntity(entity, raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.runQuery(w, q)
}

// entityQueryRaw extracts the raw AST value for an entity request, or nil when
// no query was supplied.
func entityQueryRaw(r *http.Request) (any, error) {
	switch r.Method {
	case http.MethodGet:
		qs := r.URL.Query().Get("query")
		if qs == "" {
			return nil, nil
		}
		var v any
		if err := json.Unmarshal([]byte(qs), &v); err != nil {
			return nil, fmt.Errorf("invalid query parameter: %v", err)
		}
		return v, nil
	case http.MethodPost:
		data, _ := io.ReadAll(r.Body)
		if len(strings.TrimSpace(string(data))) == 0 {
			return nil, nil
		}
		var body struct {
			Query any `json:"query"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return nil, fmt.Errorf("invalid request body: %v", err)
		}
		return body.Query, nil
	default:
		return nil, fmt.Errorf("entity endpoint requires GET or POST")
	}
}

// queryForEntity builds a query for entity from a raw AST value: nil (all rows),
// a full ["from", ...] query, an ["extract", ...] node, or a bare filter clause.
func queryForEntity(entity string, raw any) (*Query, error) {
	if raw == nil {
		return &Query{Entity: entity}, nil
	}
	if arr, ok := raw.([]any); ok && len(arr) > 0 {
		if head, ok := arr[0].(string); ok && head == "from" {
			return queryFromAST(arr)
		}
	}
	q := &Query{Entity: entity}
	if err := applyInner(q, raw); err != nil {
		return nil, err
	}
	return q, nil
}

// runQuery evaluates q against the store and writes the JSON result rows.
func (s *Server) runQuery(w http.ResponseWriter, q *Query) {
	s.mu.RLock()
	rows, err := s.store.Eval(q)
	s.mu.RUnlock()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeRows(w, rows)
}

// handleCommand serves POST /pdb/cmd/v1, ingesting a command payload. The
// command name (underscores standing in for spaces), version and certname come
// from the query string; the payload is the request body.
func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "command endpoint requires POST")
		return
	}
	params := r.URL.Query()
	command := strings.ReplaceAll(params.Get("command"), "_", " ")
	if command == "" {
		writeError(w, http.StatusBadRequest, "missing command parameter")
		return
	}
	version, err := strconv.Atoi(params.Get("version"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid version parameter")
		return
	}
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "reading payload: "+err.Error())
		return
	}

	s.mu.Lock()
	if err := s.store.Ingest(command, version, payload); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.store.path != "" {
		if err := s.store.Save(); err != nil {
			s.mu.Unlock()
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"uuid": s.genUID()})
}

// writeRows writes result rows as a JSON array, never null.
func writeRows(w http.ResponseWriter, rows []Row) {
	if rows == nil {
		rows = []Row{}
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(rows)
}

// writeError writes a JSON error object with the given HTTP status.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// newUUID returns a random RFC 4122 version-4 UUID string.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
