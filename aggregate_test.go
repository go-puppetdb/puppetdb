// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"reflect"
	"testing"
)

// aggFixture builds a facts dataset with three groups (os, kernel, empty) whose
// value column mixes numbers, a missing field and an explicit null, so the
// aggregate functions exercise their present/absent/non-numeric branches.
func aggFixture() *Store {
	s := NewStore()
	s.Add("facts",
		Row{"certname": "web1", "name": "os", "value": 100.0},
		Row{"certname": "web2", "name": "os", "value": 50.0},
		Row{"certname": "web3", "name": "os", "value": 200.0},
		Row{"certname": "db1", "name": "kernel", "value": 10.0},
		Row{"certname": "db2", "name": "kernel"},               // value missing
		Row{"certname": "db3", "name": "kernel", "value": nil}, // value null
		Row{"certname": "x1", "name": "empty"},
		Row{"certname": "x2", "name": "empty", "value": nil},
	)
	return s
}

func TestAggregateGroupBy(t *testing.T) {
	s := aggFixture()
	cases := []struct {
		name string
		pql  string
		want []Row
	}{
		{"count all", `facts[count()]{}`,
			[]Row{{"count": 8}}},
		{"count empty result", `facts[count()]{ name = "nope" }`,
			[]Row{{"count": 0}}},
		{"count by name", `facts[name, count()]{ group by name }`,
			[]Row{{"name": "empty", "count": 2}, {"name": "kernel", "count": 3}, {"name": "os", "count": 3}}},
		{"count field by name", `facts[name, count(value)]{ group by name }`,
			[]Row{{"name": "empty", "count": 0}, {"name": "kernel", "count": 1}, {"name": "os", "count": 3}}},
		{"sum by name", `facts[name, sum(value)]{ group by name }`,
			[]Row{{"name": "empty", "sum": nil}, {"name": "kernel", "sum": 10.0}, {"name": "os", "sum": 350.0}}},
		{"avg by name", `facts[name, avg(value)]{ group by name }`,
			[]Row{{"name": "empty", "avg": nil}, {"name": "kernel", "avg": 10.0}, {"name": "os", "avg": 350.0 / 3.0}}},
		{"min by name", `facts[name, min(value)]{ group by name }`,
			[]Row{{"name": "empty", "min": nil}, {"name": "kernel", "min": 10.0}, {"name": "os", "min": 50.0}}},
		{"max by name", `facts[name, max(value)]{ group by name }`,
			[]Row{{"name": "empty", "max": nil}, {"name": "kernel", "max": 10.0}, {"name": "os", "max": 200.0}}},
		{"string min max", `facts[min(name), max(name)]{}`,
			[]Row{{"min": "empty", "max": "os"}}},
		{"group by no extract", `facts{ group by name }`,
			[]Row{{"name": "empty"}, {"name": "kernel"}, {"name": "os"}}},
		{"group by two", `facts[name, certname, count()]{ certname = "web1" group by name, certname }`,
			[]Row{{"name": "os", "certname": "web1", "count": 1}}},
		{"aggregate then limit", `facts[name, count()]{ group by name } limit 1`,
			[]Row{{"name": "empty", "count": 2}}},
		{"aggregate then offset", `facts[name, count()]{ group by name } offset 2`,
			[]Row{{"name": "os", "count": 3}}},
		{"aggregate order desc", `facts[name, count()]{ group by name } order by name desc`,
			[]Row{{"name": "os", "count": 3}, {"name": "kernel", "count": 3}, {"name": "empty", "count": 2}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.Query(tc.pql)
			if err != nil {
				t.Fatalf("Query(%q): %v", tc.pql, err)
			}
			if !reflect.DeepEqual(rows, tc.want) {
				t.Fatalf("pql %q:\n got  %v\n want %v", tc.pql, rows, tc.want)
			}
		})
	}
}

// TestAggregateNonComparableMinMax covers min/max over a boolean column, where
// orderedCompare reports the pair as non-comparable and the running best stays
// put (returning the first seen value).
func TestAggregateNonComparableMinMax(t *testing.T) {
	s := NewStore()
	s.Add("nodes",
		Row{"certname": "a", "flag": true},
		Row{"certname": "b", "flag": false},
	)
	rows, err := s.Query(`nodes[min(flag)]{}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["min"] != true {
		t.Fatalf("min(flag): got %v want [{min:true}]", rows)
	}
}

func TestAggregateErrors(t *testing.T) {
	s := aggFixture()
	// Populate the outer entities so the filter actually reaches these nodes.
	s.Add("nodes", Row{"certname": "web1"})
	s.Add("fact_contents", Row{"certname": "n1", "path": []any{"x"}})
	cases := map[string]string{
		"avg no arg":          `facts[avg()]{}`,
		"sum no arg":          `facts[sum()]{}`,
		"min no arg":          `facts[min()]{}`,
		"grouped avg no arg":  `facts[name, avg()]{ group by name }`,
		"to_string eval":      `facts[to_string(value, "X")]{}`,
		"group by function":   `facts[count()]{ group by to_string(name, "X") }`,
		"in subquery func":    `nodes{ certname in facts[count()]{} }`,
		"implicit subquery":   `nodes{ facts { name = "os" } }`,
		"regexp array bad re": `fact_contents{ path ~> ["["] }`,
	}
	for name, pql := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Query(pql); err == nil {
				t.Fatalf("expected eval error for %q", pql)
			}
		})
	}
}

// TestRegexpArrayEval covers the ~> operator against array, non-array, missing,
// length-mismatch and non-string-element rows.
func TestRegexpArrayEval(t *testing.T) {
	s := NewStore()
	s.Add("fact_contents",
		Row{"certname": "n1", "path": []any{"networking", "eth0", "ip"}},
		Row{"certname": "n2", "path": []any{"networking", "lo"}},
		Row{"certname": "n3", "path": "notarray"},
		Row{"certname": "n4"},
		Row{"certname": "n5", "path": []any{"networking", 42}},
	)
	cases := []struct {
		name string
		pql  string
		want []string
	}{
		{"exact", `fact_contents[certname]{ path ~> ["networking", "eth0", "ip"] }`, []string{"n1"}},
		{"wildcard len2", `fact_contents[certname]{ path ~> ["networking", ".*"] }`, []string{"n2"}},
		{"no match", `fact_contents[certname]{ path ~> ["nope"] }`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.Query(tc.pql)
			if err != nil {
				t.Fatalf("Query(%q): %v", tc.pql, err)
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

// TestSelectSubqueryEval confirms the legacy select_ spelling evaluates
// identically to the modern subquery form.
func TestSelectSubqueryEval(t *testing.T) {
	s := NewStore()
	s.Add("nodes", Row{"certname": "web1"}, Row{"certname": "db1"})
	s.Add("resources",
		Row{"certname": "web1", "type": "Class"},
		Row{"certname": "db1", "type": "File"},
	)
	rows, err := s.Query(`nodes[certname]{ certname in select_resources[certname]{ type = "Class" } }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["certname"] != "web1" {
		t.Fatalf("select_ subquery: got %v want [web1]", rows)
	}
}
