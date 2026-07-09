// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import "testing"

func TestParseAndCompile(t *testing.T) {
	cases := []struct {
		name string
		pql  string
		want string
	}{
		{"simple eq", `nodes { certname = "web1.example.com" }`,
			`["from","nodes",["=","certname","web1.example.com"]]`},
		{"projection with filter", `nodes[certname]{ certname = "a" }`,
			`["from","nodes",["extract",["certname"],["=","certname","a"]]]`},
		{"projection no filter", `nodes[certname, report_timestamp]{}`,
			`["from","nodes",["extract",["certname","report_timestamp"]]]`},
		{"resources and", `resources{ type = "File" and title = "/etc/passwd" }`,
			`["from","resources",["and",["=","type","File"],["=","title","/etc/passwd"]]]`},
		{"facts and", `facts{ name = "osfamily" and value = "RedHat" }`,
			`["from","facts",["and",["=","name","osfamily"],["=","value","RedHat"]]]`},
		{"neq", `nodes{ a != 1 }`, `["from","nodes",["not",["=","a",1]]]`},
		{"regexp match", `nodes{ a ~ "x" }`, `["from","nodes",["~","a","x"]]`},
		{"regexp nonmatch", `nodes{ a !~ "x" }`, `["from","nodes",["not",["~","a","x"]]]`},
		{"precedence", `nodes{ a < 1 and b > 2 or c <= 3 and d >= 4 }`,
			`["from","nodes",["or",["and",["<","a",1],[">","b",2]],["and",["<=","c",3],[">=","d",4]]]]`},
		{"not prefix", `nodes{ not a = 1 }`, `["from","nodes",["not",["=","a",1]]]`},
		{"dotted field", `nodes{ facts.os.family = "RedHat" }`,
			`["from","nodes",["=","facts.os.family","RedHat"]]`},
		{"is null", `nodes{ a is null }`, `["from","nodes",["null?","a",true]]`},
		{"is not null", `nodes{ a is not null }`, `["from","nodes",["null?","a",false]]`},
		{"bool literal", `nodes{ a = true }`, `["from","nodes",["=","a",true]]`},
		{"bool false", `nodes{ a = false }`, `["from","nodes",["=","a",false]]`},
		{"null literal", `nodes{ a = null }`, `["from","nodes",["=","a",null]]`},
		{"subquery in", `nodes{ certname in resources[certname]{ type = "Class" } }`,
			`["from","nodes",["in","certname",["from","resources",["extract",["certname"],["=","type","Class"]]]]]`},
		{"multi-field subquery in", `nodes{ [certname, environment] in resources[certname, environment]{ type = "Class" } }`,
			`["from","nodes",["in",["certname","environment"],["from","resources",["extract",["certname","environment"],["=","type","Class"]]]]]`},
		{"array in", `nodes{ a in [1, 2, 3] }`,
			`["from","nodes",["in","a",["array",[1,2,3]]]]`},
		{"empty array in", `nodes{ a in [] }`,
			`["from","nodes",["in","a",["array",[]]]]`},
		{"mixed array in", `nodes{ a in ["x", true, null] }`,
			`["from","nodes",["in","a",["array",["x",true,null]]]]`},
		{"parens", `nodes{ (a = 1 or b = 2) and c = 3 }`,
			`["from","nodes",["and",["or",["=","a",1],["=","b",2]],["=","c",3]]]`},
		{"order only", `nodes{} order by certname`,
			`["from","nodes",["order_by",[["certname","asc"]]]]`},
		{"order paging", `nodes{} order by a desc, b asc limit 10 offset 5`,
			`["from","nodes",["order_by",[["a","desc"],["b","asc"]]],["limit",10],["offset",5]]`},
		{"limit after filter", `nodes{ a = 1 } limit 2`,
			`["from","nodes",["=","a",1],["limit",2]]`},
		{"offset after extract", `nodes[certname]{ a = 1 } offset 3`,
			`["from","nodes",["extract",["certname"],["=","a",1]],["offset",3]]`},
		{"empty", `nodes{}`, `["from","nodes"]`},
		{"negative number", `nodes{ a = -3.5 }`, `["from","nodes",["=","a",-3.5]]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := Parse(tc.pql)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.pql, err)
			}
			got := string(q.MarshalAST())
			if got != tc.want {
				t.Fatalf("AST mismatch\n pql:  %s\n got:  %s\n want: %s", tc.pql, got, tc.want)
			}
		})
	}
}

func TestParseAllEntities(t *testing.T) {
	for ent := range entities {
		if _, err := Parse(ent + "{}"); err != nil {
			t.Fatalf("entity %q: %v", ent, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no entity":            `{}`,
		"unknown entity":       `foo{}`,
		"lexer error":          `@`,
		"expected brace ident": `nodes x`,
		"expected brace eof":   `nodes`,
		"projection field err": `nodes[.]{}`,
		"projection rbracket":  `nodes[a b]{}`,
		"primary no op":        `nodes{ a }`,
		"primary unexpected":   `nodes{ = 1 }`,
		"literal missing":      `nodes{ a = }`,
		"literal identifier":   `nodes{ a = foo }`,
		"field after dot":      `nodes{ a. = 1 }`,
		"rparen missing":       `nodes{ (a = 1 }`,
		"paren inner err":      `nodes{ ( }`,
		"trailing after expr":  `nodes{ a = 1 x }`,
		"rbrace missing":       `nodes{ a = 1 `,
		"not inner err":        `nodes{ not }`,
		"and right err":        `nodes{ a = 1 and }`,
		"or right err":         `nodes{ a = 1 or }`,
		"is bad keyword":       `nodes{ a is foo }`,
		"is not bad keyword":   `nodes{ a is not foo }`,
		"fieldlist no in":      `nodes{ [a,b] = 1 }`,
		"empty field list":     `nodes{ [] in resources[certname]{a=1} }`,
		"fieldlist field err":  `nodes{ [.] in resources[certname]{a=1} }`,
		"fieldlist rbracket":   `nodes{ [a b] in resources[certname]{a=1} }`,
		"in bad rhs":           `nodes{ a in 5 }`,
		"in subquery unknown":  `nodes{ a in foo{a=1} }`,
		"in subquery no proj":  `nodes{ a in resources{ type = "Class" } }`,
		"array element err":    `nodes{ a in [foo] }`,
		"array rbracket":       `nodes{ a in [1 2] }`,
		"order by missing":     `nodes{} order x`,
		"order field err":      `nodes{} order by .`,
		"limit not number":     `nodes{} limit x`,
		"limit not integer":    `nodes{} limit 1.5`,
		"limit negative":       `nodes{} limit -3`,
		"offset err":           `nodes{} offset x`,
		"trailing token":       `nodes{} foo`,
		"literal at eof":       `nodes{ a =`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(src); err == nil {
				t.Fatalf("expected parse error for %q", src)
			}
		})
	}
}
