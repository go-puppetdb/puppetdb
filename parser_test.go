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

		// --- aggregate functions and group by ---
		{"count only", `nodes[count()]{}`,
			`["from","nodes",["extract",[["function","count"]]]]`},
		{"count field", `facts[count(value)]{}`,
			`["from","facts",["extract",[["function","count","value"]]]]`},
		{"field and count group by", `facts[name, count()] { group by name }`,
			`["from","facts",["extract",["name",["function","count"]],["group_by","name"]]]`},
		{"count with filter and group by", `facts[name, count(value)] { certname ~ "^web" group by name }`,
			`["from","facts",["extract",["name",["function","count","value"]],["~","certname","^web"],["group_by","name"]]]`},
		{"avg sum min max", `facts[avg(value), sum(value), min(value), max(value)]{}`,
			`["from","facts",["extract",[["function","avg","value"],["function","sum","value"],["function","min","value"],["function","max","value"]]]]`},
		{"to_string function", `reports[count(), to_string(receive_time, "DAY")]{group by to_string(receive_time, "DAY")}`,
			`["from","reports",["extract",[["function","count"],["function","to_string","receive_time","DAY"]],["group_by",["function","to_string","receive_time","DAY"]]]]`},
		{"group by two fields", `resources[type, title, count()]{ group by type, title }`,
			`["from","resources",["extract",["type","title",["function","count"]],["group_by","type","title"]]]`},
		{"group by no extract", `facts{ group by name }`,
			`["from","facts",["group_by","name"]]`},
		{"group by outside braces", `facts[name, count()]{} group by name`,
			`["from","facts",["extract",["name",["function","count"]],["group_by","name"]]]`},
		{"group by with paging", `facts[name, count()]{ group by name } order by count desc limit 5`,
			`["from","facts",["extract",["name",["function","count"]],["group_by","name"]],["order_by",[["count","desc"]]],["limit",5]]`},

		// --- paging inside braces (real PQL grammar) ---
		{"limit inside braces", `nodes[certname]{ limit 3 }`,
			`["from","nodes",["extract",["certname"]],["limit",3]]`},
		{"filter and paging inside", `nodes{ a = 1 order by a limit 2 offset 1 }`,
			`["from","nodes",["=","a",1],["order_by",[["a","asc"]]],["limit",2],["offset",1]]`},

		// --- legacy select_ subquery spelling ---
		{"select_ subquery", `nodes{ certname in select_resources[certname]{ type = "Class" } }`,
			`["from","nodes",["in","certname",["from","resources",["extract",["certname"],["=","type","Class"]]]]]`},

		// --- implicit subqueries ---
		{"implicit subquery", `nodes{ resources { type = "Class" } }`,
			`["from","nodes",["subquery","resources",["=","type","Class"]]]`},
		{"implicit subquery empty", `nodes{ resources {} }`,
			`["from","nodes",["subquery","resources"]]`},
		{"implicit subquery composed", `nodes{ certname ~ "^web" and resources { type = "Package" } }`,
			`["from","nodes",["and",["~","certname","^web"],["subquery","resources",["=","type","Package"]]]]`},
		{"entity-named field", `resources{ nodes = 1 }`,
			`["from","resources",["=","nodes",1]]`},

		// --- ~> regexp array ---
		{"regexp array", `fact_contents{ path ~> ["networking", "eth0", ".*"] }`,
			`["from","fact_contents",["~>","path",["networking","eth0",".*"]]]`},

		// --- scientific notation and single quotes ---
		{"sci notation positive", `nodes{ a = 1.5e3 }`, `["from","nodes",["=","a",1500]]`},
		{"sci notation caps signed", `nodes{ a = 2E-4 }`, `["from","nodes",["=","a",0.0002]]`},
		{"sci notation int mantissa", `nodes{ a = 3e2 }`, `["from","nodes",["=","a",300]]`},
		{"sci notation plus", `nodes{ a = 1.0e+2 }`, `["from","nodes",["=","a",100]]`},
		{"single-quoted string", `nodes{ a = 'hi there' }`, `["from","nodes",["=","a","hi there"]]`},
		{"single-quoted escape", `nodes{ a = 'it\'s' }`, `["from","nodes",["=","a","it's"]]`},
		{"question-mark field", `nodes{ deactivated? = true }`, `["from","nodes",["=","deactivated?",true]]`},

		// --- additional entities ---
		{"packages entity", `packages{}`, `["from","packages"]`},
		{"factsets entity", `factsets{}`, `["from","factsets"]`},
		{"empty projection", `nodes[]{}`, `["from","nodes"]`},
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
		// aggregate / function / clause errors
		"func no rparen":        `nodes[count(a]{}`,
		"func arg field err":    `nodes[count(.)]{}`,
		"proj item field err":   `nodes[.]{}`,
		"group by no by":        `facts[name]{ group name }`,
		"group by field err":    `facts[name]{ group by . }`,
		"limit inside not int":  `nodes{ limit x }`,
		"offset inside not int": `nodes{ offset x }`,
		"order inside err":      `nodes{ order x }`,
		// select_ / subquery errors
		"select unknown entity":   `nodes{ a in select_foo[a]{a=1} }`,
		"implicit sub filter err": `nodes{ resources { bad } }`,
		"implicit sub no rbrace":  `nodes{ resources { a = 1 `,
		// ~> errors
		"regexp array no bracket":  `nodes{ path ~> "x" }`,
		"regexp array not string":  `nodes{ path ~> [1] }`,
		"regexp array no rbracket": `nodes{ path ~> ["a" "b"] }`,
		// scientific notation lexer error
		"bad exponent":     `nodes{ a = 1.5e }`,
		"bad exponent int": `nodes{ a = 3e+ }`,
		// unterminated single quote
		"unterminated sq": `nodes{ a = 'x }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(src); err == nil {
				t.Fatalf("expected parse error for %q", src)
			}
		})
	}
}
