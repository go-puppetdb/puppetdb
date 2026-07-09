// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"reflect"
	"testing"
)

func TestLexAllTokens(t *testing.T) {
	src := `nodes [ certname ] { a = "x\"y\n\t\r\\z" and b != -3.5 or c <= 1 } . ( ) , ~ !~ < > >=`
	toks, err := lex(src)
	if err != nil {
		t.Fatalf("lex error: %v", err)
	}
	// Spot-check a couple of decoded values and that we terminate with EOF.
	if toks[len(toks)-1].kind != tEOF {
		t.Fatalf("last token is not EOF: %+v", toks[len(toks)-1])
	}
	var gotString string
	for _, tk := range toks {
		if tk.kind == tString {
			gotString = tk.text
			break
		}
	}
	if want := "x\"y\n\t\r\\z"; gotString != want {
		t.Fatalf("string escapes: got %q want %q", gotString, want)
	}
	// Ensure both < and >= relational forms are produced.
	var ops []string
	for _, tk := range toks {
		if tk.kind == tOp {
			ops = append(ops, tk.text)
		}
	}
	want := []string{"=", "!=", "<=", "~", "!~", "<", ">", ">="}
	if !reflect.DeepEqual(ops, want) {
		t.Fatalf("ops: got %v want %v", ops, want)
	}
}

func TestLexErrors(t *testing.T) {
	cases := map[string]string{
		"unexpected char":     `@`,
		"bang alone":          `!`,
		"bang then space":     `a ! b`,
		"minus no digit":      `-`,
		"minus then letter":   `-x`,
		"malformed number":    `1.`,
		"unterminated string": `"abc`,
		"backslash at eof":    `"x\`,
		"trailing after dot":  `1.x`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := lex(src); err == nil {
				t.Fatalf("expected lex error for %q", src)
			}
		})
	}
}

func TestLexEmpty(t *testing.T) {
	toks, err := lex("   ")
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 1 || toks[0].kind != tEOF {
		t.Fatalf("expected single EOF, got %+v", toks)
	}
}
