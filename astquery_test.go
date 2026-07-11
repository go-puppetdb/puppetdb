// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import "testing"

// astRoundTripVectors are canonical PuppetDB AST-query documents. Each must
// parse via [ParseAST] and re-compile via [Query.MarshalAST] to the identical
// bytes — the byte-exact compatibility oracle, reusing the same canonical forms
// asserted by the PQL compiler tests.
var astRoundTripVectors = []string{
	`["from","nodes",["=","certname","web1.example.com"]]`,
	`["from","nodes",["extract",["certname"],["=","certname","a"]]]`,
	`["from","nodes",["extract",["certname","report_timestamp"]]]`,
	`["from","resources",["and",["=","type","File"],["=","title","/etc/passwd"]]]`,
	`["from","nodes",["not",["=","a",1]]]`,
	`["from","nodes",["~","a","x"]]`,
	`["from","nodes",["not",["~","a","x"]]]`,
	`["from","nodes",["or",["and",["<","a",1],[">","b",2]],["and",["<=","c",3],[">=","d",4]]]]`,
	`["from","nodes",["=","facts.os.family","RedHat"]]`,
	`["from","nodes",["null?","a",true]]`,
	`["from","nodes",["null?","a",false]]`,
	`["from","nodes",["=","a",true]]`,
	`["from","nodes",["=","a",false]]`,
	`["from","nodes",["=","a",null]]`,
	`["from","nodes",["in","certname",["from","resources",["extract",["certname"],["=","type","Class"]]]]]`,
	`["from","nodes",["in",["certname","environment"],["from","resources",["extract",["certname","environment"],["=","type","Class"]]]]]`,
	`["from","nodes",["in","a",["array",[1,2,3]]]]`,
	`["from","nodes",["in","a",["array",[]]]]`,
	`["from","nodes",["in","a",["array",["x",true,null]]]]`,
	`["from","nodes",["and",["or",["=","a",1],["=","b",2]],["=","c",3]]]`,
	`["from","nodes",["order_by",[["certname","asc"]]]]`,
	`["from","nodes",["order_by",[["a","desc"],["b","asc"]]],["limit",10],["offset",5]]`,
	`["from","nodes",["=","a",1],["limit",2]]`,
	`["from","nodes",["extract",["certname"],["=","a",1]],["offset",3]]`,
	`["from","nodes"]`,
	`["from","nodes",["=","a",-3.5]]`,
	// Aggregate / transform functions and group_by.
	`["from","facts",["extract",[["function","count"]]]]`,
	`["from","facts",["extract",["name",["function","count"]],["group_by","name"]]]`,
	`["from","facts",["extract",[["function","avg","value"]],["=","name","uptime"]]]`,
	`["from","nodes",["group_by","environment"]]`,
	// ~> regexp array and implicit subquery.
	`["from","fact_contents",["~>","path",["networking","eth0","macaddress"]]]`,
	`["from","resources",["subquery","catalogs"]]`,
	`["from","resources",["subquery","catalogs",["=","certname","web1"]]]`,
}

func TestParseASTRoundTrip(t *testing.T) {
	for _, want := range astRoundTripVectors {
		t.Run(want, func(t *testing.T) {
			q, err := ParseAST([]byte(want))
			if err != nil {
				t.Fatalf("ParseAST(%s): %v", want, err)
			}
			got := string(q.MarshalAST())
			if got != want {
				t.Fatalf("round trip mismatch\n want: %s\n got:  %s", want, got)
			}
		})
	}
}

// TestParseASTMatchesPQL cross-checks that every PQL vector's compiled AST parses
// back to a query whose re-compiled AST is identical: PQL -> AST -> Query -> AST.
func TestParseASTMatchesPQL(t *testing.T) {
	pqls := []string{
		`nodes { certname = "web1" }`,
		`nodes[certname]{ certname = "a" } order by certname desc limit 3 offset 1`,
		`facts[name, count()]{ value = "RedHat" } group by name`,
		`fact_contents{ path ~> ["a", "b"] }`,
		`resources{ certname in nodes[certname]{ environment = "production" } }`,
		`nodes{ [certname, environment] in resources[certname, environment]{ type = "Class" } }`,
	}
	for _, pql := range pqls {
		t.Run(pql, func(t *testing.T) {
			q, err := Parse(pql)
			if err != nil {
				t.Fatalf("Parse(%q): %v", pql, err)
			}
			want := string(q.MarshalAST())
			q2, err := ParseAST([]byte(want))
			if err != nil {
				t.Fatalf("ParseAST(%s): %v", want, err)
			}
			if got := string(q2.MarshalAST()); got != want {
				t.Fatalf("PQL/AST divergence\n want: %s\n got:  %s", want, got)
			}
		})
	}
}

// TestParseASTNary folds an n-ary and/or into an equivalent left-nested binary
// tree.
func TestParseASTNary(t *testing.T) {
	q, err := ParseAST([]byte(`["from","nodes",["and",["=","a",1],["=","b",2],["=","c",3]]]`))
	if err != nil {
		t.Fatal(err)
	}
	got := string(q.MarshalAST())
	want := `["from","nodes",["and",["and",["=","a",1],["=","b",2]],["=","c",3]]]`
	if got != want {
		t.Fatalf("n-ary fold\n want: %s\n got:  %s", want, got)
	}
}

// TestParseASTEval confirms a parsed AST query evaluates against a store just
// like its PQL twin.
func TestParseASTEval(t *testing.T) {
	s := fixture()
	q, err := ParseAST([]byte(`["from","nodes",["extract",["certname"],["~","certname","^web"]],["order_by",[["certname","asc"]]]]`))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Eval(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["certname"] != "web1" || rows[1]["certname"] != "web2" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestParseASTErrors(t *testing.T) {
	cases := map[string]string{
		"invalid json":         `[`,
		"not array":            `"nope"`,
		"too short":            `["from"]`,
		"bad head":             `["select","nodes"]`,
		"entity not string":    `["from",5]`,
		"unknown entity":       `["from","widgets"]`,
		"multiple inner":       `["from","nodes",["=","a",1],["=","b",2]]`,
		"multiple group_by":    `["from","nodes",["group_by","a"],["group_by","b"]]`,
		"order_by arity":       `["from","nodes",["order_by"]]`,
		"order_by not array":   `["from","nodes",["order_by","x"]]`,
		"order_by term shape":  `["from","nodes",["order_by",["x"]]]`,
		"order_by field type":  `["from","nodes",["order_by",[[1,"asc"]]]]`,
		"order_by direction":   `["from","nodes",["order_by",[["a","up"]]]]`,
		"limit not int":        `["from","nodes",["limit","x"]]`,
		"limit fractional":     `["from","nodes",["limit",1.5]]`,
		"limit negative":       `["from","nodes",["limit",-1]]`,
		"limit arity":          `["from","nodes",["limit",1,2]]`,
		"offset not int":       `["from","nodes",["offset","x"]]`,
		"extract short":        `["from","nodes",["extract"]]`,
		"extract cols type":    `["from","nodes",["extract","x"]]`,
		"extract proj item":    `["from","nodes",["extract",[1]]]`,
		"extract dup filter":   `["from","nodes",["extract",["a"],["=","a",1],["=","b",2]]]`,
		"extract bad filter":   `["from","nodes",["extract",["a"],["nope","a",1]]]`,
		"extract bad group":    `["from","nodes",["extract",["a"],["group_by",1]]]`,
		"func short":           `["from","nodes",["extract",[[]]]]`,
		"func bad head":        `["from","nodes",["extract",[["nope","count"]]]]`,
		"func name type":       `["from","nodes",["extract",[["function",1]]]]`,
		"func unknown":         `["from","nodes",["extract",[["function","median","x"]]]]`,
		"func arg type":        `["from","nodes",["extract",[["function","avg",1]]]]`,
		"group_by item":        `["from","nodes",["group_by",1]]`,
		"filter not array":     `["from","nodes","oops"]`,
		"filter op not string": `["from","nodes",[1,"a"]]`,
		"unknown operator":     `["from","nodes",["nope","a",1]]`,
		"cmp arity":            `["from","nodes",["=","a"]]`,
		"cmp field type":       `["from","nodes",["=",1,"a"]]`,
		"cmp bad literal":      `["from","nodes",["=","a",["x"]]]`,
		"logical short":        `["from","nodes",["and",["=","a",1]]]`,
		"logical left err":     `["from","nodes",["and",["nope"],["=","b",2]]]`,
		"logical right err":    `["from","nodes",["and",["=","a",1],["nope"]]]`,
		"not arity":            `["from","nodes",["not"]]`,
		"not inner err":        `["from","nodes",["not",["nope"]]]`,
		"null arity":           `["from","nodes",["null?","a"]]`,
		"null field type":      `["from","nodes",["null?",1,true]]`,
		"null flag type":       `["from","nodes",["null?","a","yes"]]`,
		"regexp arity":         `["from","nodes",["~>","a"]]`,
		"regexp field type":    `["from","nodes",["~>",1,["x"]]]`,
		"regexp pat type":      `["from","nodes",["~>","a",[1]]]`,
		"subquery arity":       `["from","nodes",["subquery"]]`,
		"subquery entity type": `["from","nodes",["subquery",1]]`,
		"subquery unknown":     `["from","nodes",["subquery","widgets"]]`,
		"subquery filter err":  `["from","nodes",["subquery","catalogs",["nope"]]]`,
		"in arity":             `["from","nodes",["in","a"]]`,
		"in lhs type":          `["from","nodes",["in",1,["array",[]]]]`,
		"in lhs empty":         `["from","nodes",["in",[],["array",[]]]]`,
		"in lhs elem type":     `["from","nodes",["in",[1],["array",[]]]]`,
		"in rhs not node":      `["from","nodes",["in","a","x"]]`,
		"in rhs unknown":       `["from","nodes",["in","a",["nope"]]]`,
		"array arity":          `["from","nodes",["in","a",["array"]]]`,
		"array not list":       `["from","nodes",["in","a",["array","x"]]]`,
		"array bad literal":    `["from","nodes",["in","a",["array",[["x"]]]]]`,
		"in subquery err":      `["from","nodes",["in","a",["from","widgets"]]]`,
		"in subquery no proj":  `["from","nodes",["in","a",["from","resources",["=","type","Class"]]]]`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAST([]byte(doc)); err == nil {
				t.Fatalf("expected error for %s", doc)
			}
		})
	}
}
