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

// TestLexNewTokens covers the ~> operator, scientific-notation numbers,
// single-quoted strings (including the \' escape and a literal backslash), and
// question-mark-terminated identifiers.
func TestLexNewTokens(t *testing.T) {
	cases := []struct {
		src  string
		kind tokenKind
		text string
	}{
		{`~>`, tOp, "~>"},
		{`1.5e3`, tNumber, "1.5e3"},
		{`2E-4`, tNumber, "2E-4"},
		{`3e2`, tNumber, "3e2"},
		{`1.0e+2`, tNumber, "1.0e+2"},
		{`'hi'`, tString, "hi"},
		{`'it\'s'`, tString, "it's"},
		{`'a\nb'`, tString, `a\nb`}, // single quotes keep a literal backslash-n
		{`deactivated?`, tIdent, "deactivated?"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			toks, err := lex(tc.src)
			if err != nil {
				t.Fatalf("lex(%q): %v", tc.src, err)
			}
			if toks[0].kind != tc.kind || toks[0].text != tc.text {
				t.Fatalf("lex(%q): got {%d,%q} want {%d,%q}", tc.src, toks[0].kind, toks[0].text, tc.kind, tc.text)
			}
		})
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
		"bad exponent":        `1.5e`,
		"bad exponent sign":   `3e+`,
		"unterminated sq":     `'abc`,
		"sq backslash at eof": `'x\`,
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
