// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"reflect"
	"sort"
	"testing"
)

// fixture builds a small dataset covering nodes, resources and facts.
func fixture() *Store {
	s := NewStore()
	s.Add("nodes",
		Row{"certname": "web1", "environment": "production", "uptime": 100.0,
			"facts": map[string]any{"os": map[string]any{"family": "RedHat"}}, "note": nil},
		Row{"certname": "web2", "environment": "production", "uptime": 50.0,
			"facts": map[string]any{"os": map[string]any{"family": "Debian"}}, "note": "hi"},
		Row{"certname": "db1", "environment": "staging", "uptime": 200.0},
	)
	s.Add("resources",
		Row{"certname": "web1", "type": "Class", "title": "nginx"},
		Row{"certname": "db1", "type": "File", "title": "/etc/passwd"},
		Row{"certname": "web1", "type": "File", "title": "/etc/nginx.conf"},
	)
	s.Add("facts",
		Row{"certname": "web1", "name": "osfamily", "value": "RedHat"},
		Row{"certname": "web2", "name": "osfamily", "value": "Debian"},
	)
	return s
}

// certnames extracts and sorts the certname column of rows for comparison.
func certnames(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r["certname"].(string))
	}
	sort.Strings(out)
	return out
}

func TestEvalFilters(t *testing.T) {
	s := fixture()
	cases := []struct {
		name string
		pql  string
		want []string
	}{
		{"eq", `nodes{ certname = "web1" }`, []string{"web1"}},
		{"neq", `nodes{ environment != "production" }`, []string{"db1"}},
		{"eq numeric", `nodes{ uptime = 50 }`, []string{"web2"}},
		{"lt", `nodes{ uptime < 100 }`, []string{"web2"}},
		{"gt", `nodes{ uptime > 100 }`, []string{"db1"}},
		{"lte", `nodes{ uptime <= 100 }`, []string{"web1", "web2"}},
		{"gte", `nodes{ uptime >= 100 }`, []string{"db1", "web1"}},
		{"and", `nodes{ environment = "production" and uptime > 60 }`, []string{"web1"}},
		{"or", `nodes{ uptime < 60 or uptime > 150 }`, []string{"db1", "web2"}},
		{"not", `nodes{ not environment = "production" }`, []string{"db1"}},
		{"regexp", `nodes{ certname ~ "^web" }`, []string{"web1", "web2"}},
		{"regexp non-match", `nodes{ certname !~ "^web" }`, []string{"db1"}},
		{"dotted", `nodes{ facts.os.family = "RedHat" }`, []string{"web1"}},
		{"is null", `nodes{ note is null }`, []string{"db1", "web1"}},
		{"is not null", `nodes{ note is not null }`, []string{"web2"}},
		{"in array", `nodes{ certname in ["web1", "db1"] }`, []string{"db1", "web1"}},
		{"in empty array", `nodes{ certname in [] }`, nil},
		{"subquery", `nodes{ certname in resources[certname]{ type = "File" } }`, []string{"db1", "web1"}},
		{"multi subquery", `nodes[certname]{ [certname] in resources[certname]{ type = "Class" } }`, []string{"web1"}},
		{"missing field eq", `nodes{ uptime = 999 }`, nil},
		{"missing field lt", `nodes{ missing < 5 }`, nil},
		{"type-mismatch order", `nodes{ certname < 5 }`, nil},
		{"non-string regexp", `nodes{ uptime ~ "1" }`, nil},
		{"all", `nodes{}`, []string{"db1", "web1", "web2"}},
		{"unknown entity data", `catalogs{}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.Query(tc.pql)
			if err != nil {
				t.Fatalf("Query(%q): %v", tc.pql, err)
			}
			if got := certnames(rows); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pql %q: got %v want %v", tc.pql, got, tc.want)
			}
		})
	}
}

func TestEvalProjection(t *testing.T) {
	s := fixture()
	rows, err := s.Query(`nodes[certname, missing]{ certname = "web1" }`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{{"certname": "web1", "missing": nil}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("got %v want %v", rows, want)
	}
}

func TestEvalPaging(t *testing.T) {
	s := fixture()
	cases := []struct {
		name string
		pql  string
		want []string
	}{
		{"order asc", `nodes[certname]{} order by certname`, []string{"db1", "web1", "web2"}},
		{"order desc", `nodes[certname]{} order by certname desc`, []string{"web2", "web1", "db1"}},
		{"limit", `nodes[certname]{} order by certname limit 2`, []string{"db1", "web1"}},
		{"offset", `nodes[certname]{} order by certname offset 1`, []string{"web1", "web2"}},
		{"offset over", `nodes[certname]{} order by certname offset 10`, nil},
		{"limit over", `nodes[certname]{} order by certname limit 10`, []string{"db1", "web1", "web2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.Query(tc.pql)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, r := range rows {
				got = append(got, r["certname"].(string))
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pql %q: got %v want %v", tc.pql, got, tc.want)
			}
		})
	}
}

// TestEvalOrderTieAndTypes exercises the multi-term tie-break, nil ordering and
// non-comparable (boolean) ordering branches of compareAny.
func TestEvalOrderTieAndTypes(t *testing.T) {
	s := NewStore()
	s.Add("nodes",
		Row{"certname": "a", "grp": "x", "opt": nil, "flag": true},
		Row{"certname": "b", "grp": "x", "opt": "z", "flag": false},
		Row{"certname": "c", "grp": "y", "opt": nil, "flag": true},
	)
	// Tie on grp for a and b, broken by certname; then group y.
	rows, err := s.Query(`nodes[certname]{} order by grp asc, certname asc`)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{rows[0]["certname"].(string), rows[1]["certname"].(string), rows[2]["certname"].(string)}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tie-break: got %v want %v", got, want)
	}
	// opt has nil values (sort first) and a string; covers nil vs non-nil.
	if _, err := s.Query(`nodes[certname]{} order by opt asc`); err != nil {
		t.Fatal(err)
	}
	// flag is boolean: orderedCompare reports non-comparable, order stays stable.
	if _, err := s.Query(`nodes[certname]{} order by flag asc`); err != nil {
		t.Fatal(err)
	}
	// nil-first vs nil-second both directions via desc.
	if _, err := s.Query(`nodes[certname]{} order by opt desc`); err != nil {
		t.Fatal(err)
	}
}

func TestEvalErrors(t *testing.T) {
	s := fixture()
	cases := map[string]string{
		"parse error":         `nodes{`,
		"bad regexp":          `nodes{ certname ~ "[" }`,
		"bad regexp in not":   `nodes{ not certname ~ "[" }`,
		"bad regexp in and":   `nodes{ certname ~ "[" and certname = "web1" }`,
		"bad regexp in or":    `nodes{ certname ~ "[" or certname = "web1" }`,
		"subquery eval error": `nodes{ certname in resources[certname]{ type ~ "[" } }`,
		"arity mismatch":      `nodes{ [certname, environment] in resources[certname]{ type = "File" } }`,
	}
	for name, pql := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Query(pql); err == nil {
				t.Fatalf("expected eval error for %q", pql)
			}
		})
	}
}

// TestEvalArrayMultiField covers the array-membership arity guard, which the
// grammar cannot reach directly, by constructing the AST by hand.
func TestEvalArrayMultiField(t *testing.T) {
	s := fixture()
	q := &Query{
		Entity: "nodes",
		Filter: In{Fields: []string{"certname", "environment"}, Array: []Literal{{Kind: LitString, Str: "web1"}}},
	}
	if _, err := s.Eval(q); err == nil {
		t.Fatal("expected arity error for multi-field array membership")
	}
}

// TestEvalLogicalShortCircuit covers the and/or short-circuit branches.
func TestEvalLogicalShortCircuit(t *testing.T) {
	s := fixture()
	// and: left false short-circuits; or: left true short-circuits.
	if rows, _ := s.Query(`nodes{ certname = "nope" and uptime > 0 }`); len(rows) != 0 {
		t.Fatalf("and short-circuit: got %d rows", len(rows))
	}
	if rows, _ := s.Query(`nodes{ certname = "web1" or uptime > 999 }`); certnames(rows)[0] != "web1" {
		t.Fatalf("or short-circuit failed")
	}
}

// TestDigFieldNonMap covers digging a dotted path through a non-map value.
func TestDigFieldNonMap(t *testing.T) {
	s := NewStore()
	s.Add("nodes", Row{"certname": "a", "facts": "notamap"})
	rows, err := s.Query(`nodes{ facts.os = "x" }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no match digging into non-map, got %d", len(rows))
	}
}
