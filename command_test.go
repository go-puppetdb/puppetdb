// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"reflect"
	"testing"
)

const factsV5 = `{
  "certname": "web1.example.com",
  "environment": "production",
  "producer_timestamp": "2026-07-11T10:00:00Z",
  "producer": "puppet.example.com",
  "values": {
    "osfamily": "RedHat",
    "uptime_seconds": 3600,
    "networking": {"interfaces": {"eth0": {"ip": "10.0.0.1"}}},
    "processors": ["c0", "c1"],
    "trusted": {"certname": "web1.example.com"}
  }
}`

func TestIngestFacts(t *testing.T) {
	s := NewStore()
	if err := s.Ingest(CmdReplaceFacts, 5, []byte(factsV5)); err != nil {
		t.Fatalf("ingest facts: %v", err)
	}

	// One facts row per top-level fact.
	rows, err := s.Query(`facts{ certname = "web1.example.com" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("want 5 facts rows, got %d", len(rows))
	}

	// fact_contents flattens structured facts to leaf paths.
	fc, err := s.Query(`fact_contents{ name = "ip" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fc) != 1 {
		t.Fatalf("want 1 ip fact_content, got %d", len(fc))
	}
	wantPath := []any{"networking", "interfaces", "eth0", "ip"}
	if !reflect.DeepEqual(fc[0]["path"], wantPath) {
		t.Fatalf("path: got %v want %v", fc[0]["path"], wantPath)
	}
	if fc[0]["value"] != "10.0.0.1" {
		t.Fatalf("value: got %v", fc[0]["value"])
	}

	// Array facts flatten with integer indices; the leaf name is the index.
	arr, err := s.Query(`fact_contents{ name = "1" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 || arr[0]["value"] != "c1" {
		t.Fatalf("array fact_content: %v", arr)
	}

	// inventory holds the whole values object and the trusted sub-object.
	inv, err := s.Query(`inventory{ certname = "web1.example.com" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv) != 1 {
		t.Fatalf("want 1 inventory row, got %d", len(inv))
	}
	trusted, _ := inv[0]["trusted"].(map[string]any)
	if trusted["certname"] != "web1.example.com" {
		t.Fatalf("trusted: %v", inv[0]["trusted"])
	}

	// The node gains facts environment/timestamp.
	nodes, err := s.Query(`nodes{ facts_environment = "production" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0]["certname"] != "web1.example.com" {
		t.Fatalf("node: %v", nodes)
	}
}

func TestIngestFactsReplaces(t *testing.T) {
	s := NewStore()
	_ = s.Ingest(CmdReplaceFacts, 5, []byte(`{"certname":"n1","environment":"e","producer_timestamp":"t","values":{"a":"1","b":"2"}}`))
	// A second node's facts coexist; replacing n1 must keep n2's rows (the
	// "rows for other certnames remain" branch of replaceFor).
	_ = s.Ingest(CmdReplaceFacts, 5, []byte(`{"certname":"n2","environment":"e","producer_timestamp":"t","values":{"z":"9"}}`))
	// A second replace-facts for the same node supersedes the first.
	_ = s.Ingest(CmdReplaceFacts, 5, []byte(`{"certname":"n1","environment":"e","producer_timestamp":"t","values":{"a":"9"}}`))
	if n2, _ := s.Query(`facts{ certname = "n2" }`); len(n2) != 1 {
		t.Fatalf("n2 facts should survive n1 replace, got %d", len(n2))
	}
	rows, _ := s.Query(`facts{ certname = "n1" }`)
	if len(rows) != 1 {
		t.Fatalf("want 1 fact after replace, got %d", len(rows))
	}
	if rows[0]["name"] != "a" || rows[0]["value"] != "9" {
		t.Fatalf("stale fact after replace: %v", rows[0])
	}
	// A single node row remains (upsert, not duplicate).
	nodes, _ := s.Query(`nodes{ certname = "n1" }`)
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
}

const catalogV9 = `{
  "certname": "web1.example.com",
  "version": "1626000000",
  "environment": "production",
  "transaction_uuid": "aaaa",
  "catalog_uuid": "bbbb",
  "code_id": "cccc",
  "producer_timestamp": "2026-07-11T10:00:00Z",
  "producer": "puppet.example.com",
  "resources": [
    {"type":"Class","title":"nginx","exported":false,"tags":["nginx"],"file":"/m.pp","line":10,"parameters":{"ensure":"present"}},
    {"type":"File","title":"/etc/nginx.conf","exported":false,"parameters":{}}
  ],
  "edges": [
    {"source":{"type":"Class","title":"nginx"},"target":{"type":"File","title":"/etc/nginx.conf"},"relationship":"contains"}
  ]
}`

func TestIngestCatalog(t *testing.T) {
	s := NewStore()
	if err := s.Ingest(CmdReplaceCatalog, 9, []byte(catalogV9)); err != nil {
		t.Fatalf("ingest catalog: %v", err)
	}

	res, err := s.Query(`resources{ certname = "web1.example.com" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 resources, got %d", len(res))
	}
	cls, _ := s.Query(`resources{ type = "Class" and title = "nginx" }`)
	if len(cls) != 1 || cls[0]["line"] != 10.0 || cls[0]["file"] != "/m.pp" {
		t.Fatalf("class resource: %v", cls)
	}
	if _, ok := cls[0]["resource"].(string); !ok {
		t.Fatalf("resource hash missing: %v", cls[0]["resource"])
	}
	tags, _ := cls[0]["tags"].([]any)
	if len(tags) != 1 || tags[0] != "nginx" {
		t.Fatalf("tags: %v", cls[0]["tags"])
	}
	// A resource with a null file/line surfaces nil.
	f, _ := s.Query(`resources{ type = "File" }`)
	if f[0]["file"] != nil || f[0]["line"] != nil {
		t.Fatalf("want nil file/line, got %v/%v", f[0]["file"], f[0]["line"])
	}

	edges, err := s.Query(`edges{ certname = "web1.example.com" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0]["relationship"] != "contains" ||
		edges[0]["source_title"] != "nginx" || edges[0]["target_type"] != "File" {
		t.Fatalf("edge: %v", edges)
	}

	cat, err := s.Query(`catalogs{ certname = "web1.example.com" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) != 1 || cat[0]["transaction_uuid"] != "aaaa" || cat[0]["producer"] != "puppet.example.com" {
		t.Fatalf("catalog: %v", cat)
	}
	nodes, _ := s.Query(`nodes{ catalog_environment = "production" }`)
	if len(nodes) != 1 {
		t.Fatalf("want 1 node with catalog env, got %d", len(nodes))
	}
}

const reportV8 = `{
  "certname": "web1.example.com",
  "environment": "production",
  "puppet_version": "8.5.0",
  "report_format": 12,
  "configuration_version": "1626000000",
  "start_time": "2026-07-11T10:00:00Z",
  "end_time": "2026-07-11T10:01:00Z",
  "producer_timestamp": "2026-07-11T10:01:05Z",
  "producer": "puppet.example.com",
  "transaction_uuid": "aaaa",
  "catalog_uuid": "bbbb",
  "code_id": "cccc",
  "cached_catalog_status": "not_used",
  "status": "changed",
  "noop": false,
  "noop_pending": false,
  "corrective_change": false,
  "resources": [
    {"resource_type":"File","resource_title":"/etc/nginx.conf","skipped":false,"file":"/m.pp","line":10,"containment_path":["Stage[main]","Nginx"],
     "events":[
       {"status":"success","timestamp":"2026-07-11T10:00:30Z","property":"ensure","name":null,"new_value":"present","old_value":"absent","message":"created","corrective_change":false}
     ]},
    {"resource_type":"Exec","resource_title":"reload","skipped":true,"file":null,"line":null,"containment_path":null,"events":[]}
  ]
}`

func TestIngestReport(t *testing.T) {
	s := NewStore()
	if err := s.Ingest(CmdStoreReport, 8, []byte(reportV8)); err != nil {
		t.Fatalf("ingest report: %v", err)
	}

	reps, err := s.Query(`reports{ certname = "web1.example.com" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(reps) != 1 || reps[0]["status"] != "changed" || reps[0]["puppet_version"] != "8.5.0" {
		t.Fatalf("report: %v", reps)
	}

	evs, err := s.Query(`events{ certname = "web1.example.com" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	e := evs[0]
	if e["status"] != "success" || e["property"] != "ensure" || e["new_value"] != "present" ||
		e["resource_title"] != "/etc/nginx.conf" || e["name"] != nil {
		t.Fatalf("event fields: %v", e)
	}
	cp, _ := e["containment_path"].([]any)
	if len(cp) != 2 || cp[1] != "Nginx" {
		t.Fatalf("containment path: %v", e["containment_path"])
	}

	nodes, _ := s.Query(`nodes{ latest_report_status = "changed" }`)
	if len(nodes) != 1 || nodes[0]["report_environment"] != "production" {
		t.Fatalf("node latest report: %v", nodes)
	}

	// A second report accumulates (store, not replace).
	if err := s.Ingest(CmdStoreReport, 8, []byte(reportV8)); err != nil {
		t.Fatal(err)
	}
	reps2, _ := s.Query(`reports{ certname = "web1.example.com" }`)
	if len(reps2) != 2 {
		t.Fatalf("reports should accumulate, got %d", len(reps2))
	}
}

// TestIngestReportNoCorrectiveChange covers the nil branch of boolPtr: a report
// and event that omit corrective_change surface a null.
func TestIngestReportNoCorrectiveChange(t *testing.T) {
	s := NewStore()
	payload := `{"certname":"n","environment":"e","status":"unchanged",
	  "resources":[{"resource_type":"File","resource_title":"/t","events":[
	    {"status":"noop","timestamp":"t"}]}]}`
	if err := s.Ingest(CmdStoreReport, 8, []byte(payload)); err != nil {
		t.Fatal(err)
	}
	reps, _ := s.Query(`reports{ certname = "n" }`)
	if reps[0]["corrective_change"] != nil {
		t.Fatalf("want nil corrective_change, got %v", reps[0]["corrective_change"])
	}
	evs, _ := s.Query(`events{ certname = "n" }`)
	if evs[0]["corrective_change"] != nil {
		t.Fatalf("want nil event corrective_change, got %v", evs[0]["corrective_change"])
	}
}

func TestIngestErrors(t *testing.T) {
	s := NewStore()
	cases := []struct {
		name    string
		command string
		version int
		payload string
	}{
		{"unknown command", "vacuum", 1, `{}`},
		{"facts bad version", CmdReplaceFacts, 4, `{}`},
		{"facts bad json", CmdReplaceFacts, 5, `{`},
		{"facts no certname", CmdReplaceFacts, 5, `{"values":{}}`},
		{"catalog bad version", CmdReplaceCatalog, 8, `{}`},
		{"catalog bad json", CmdReplaceCatalog, 9, `{`},
		{"catalog no certname", CmdReplaceCatalog, 9, `{"resources":[]}`},
		{"report bad version", CmdStoreReport, 7, `{}`},
		{"report bad json", CmdStoreReport, 8, `{`},
		{"report no certname", CmdStoreReport, 8, `{"resources":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Ingest(tc.command, tc.version, []byte(tc.payload)); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

// TestIngestFactsScalarAndEmptyTrusted covers a bare scalar fact (single leaf
// path) and the absence of a trusted sub-object.
func TestIngestFactsScalarAndEmptyTrusted(t *testing.T) {
	s := NewStore()
	if err := s.Ingest(CmdReplaceFacts, 5, []byte(`{"certname":"n","environment":"e","producer_timestamp":"t","values":{"kernel":"Linux"}}`)); err != nil {
		t.Fatal(err)
	}
	fc, _ := s.Query(`fact_contents{ name = "kernel" }`)
	if len(fc) != 1 {
		t.Fatalf("want 1 leaf, got %d", len(fc))
	}
	if p, _ := fc[0]["path"].([]any); len(p) != 1 || p[0] != "kernel" {
		t.Fatalf("scalar path: %v", fc[0]["path"])
	}
	inv, _ := s.Query(`inventory{ certname = "n" }`)
	if inv[0]["trusted"] != nil {
		t.Fatalf("want nil trusted, got %v", inv[0]["trusted"])
	}
}

// TestUpsertNodeExisting covers merging catalog data onto a node created by a
// prior facts ingest (the upsert-existing branch).
func TestUpsertNodeExisting(t *testing.T) {
	s := NewStore()
	_ = s.Ingest(CmdReplaceFacts, 5, []byte(`{"certname":"n","environment":"e","producer_timestamp":"t","values":{}}`))
	_ = s.Ingest(CmdReplaceCatalog, 9, []byte(`{"certname":"n","environment":"e","resources":[],"edges":[]}`))
	nodes, _ := s.Query(`nodes{ certname = "n" }`)
	if len(nodes) != 1 {
		t.Fatalf("want a single merged node, got %d", len(nodes))
	}
	if nodes[0]["facts_environment"] != "e" || nodes[0]["catalog_environment"] != "e" {
		t.Fatalf("merge lost a field: %v", nodes[0])
	}
}
