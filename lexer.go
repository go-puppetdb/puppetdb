// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"fmt"
	"strings"
)

// tokenKind enumerates the lexical token classes of PQL.
type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tString
	tNumber
	tLBracket // [
	tRBracket // ]
	tLBrace   // {
	tRBrace   // }
	tLParen   // (
	tRParen   // )
	tComma    // ,
	tDot      // .
	tOp       // = != < > <= >= ~ !~
)

// token is a single lexical unit with its source offset (for error messages).
type token struct {
	kind tokenKind
	text string
	pos  int
}

// lexer converts a PQL source string into a slice of tokens.
type lexer struct {
	src string
	pos int
}

// lex tokenises src, returning the tokens (always terminated by a tEOF token)
// or the first lexical error encountered.
func lex(src string) ([]token, error) {
	l := &lexer{src: src}
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.kind == tEOF {
			return toks, nil
		}
	}
}

// isSpace reports whether b is PQL whitespace.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// isDigit reports whether b is an ASCII digit.
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// isIdentStart reports whether b may begin an identifier.
func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isIdentPart reports whether b may continue an identifier.
func isIdentPart(b byte) bool { return isIdentStart(b) || isDigit(b) }

// next scans and returns the next token.
func (l *lexer) next() (token, error) {
	for l.pos < len(l.src) && isSpace(l.src[l.pos]) {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return token{kind: tEOF, pos: l.pos}, nil
	}
	start := l.pos
	c := l.src[l.pos]
	switch {
	case c == '[':
		l.pos++
		return token{kind: tLBracket, text: "[", pos: start}, nil
	case c == ']':
		l.pos++
		return token{kind: tRBracket, text: "]", pos: start}, nil
	case c == '{':
		l.pos++
		return token{kind: tLBrace, text: "{", pos: start}, nil
	case c == '}':
		l.pos++
		return token{kind: tRBrace, text: "}", pos: start}, nil
	case c == '(':
		l.pos++
		return token{kind: tLParen, text: "(", pos: start}, nil
	case c == ')':
		l.pos++
		return token{kind: tRParen, text: ")", pos: start}, nil
	case c == ',':
		l.pos++
		return token{kind: tComma, text: ",", pos: start}, nil
	case c == '.':
		l.pos++
		return token{kind: tDot, text: ".", pos: start}, nil
	case c == '"':
		return l.lexString()
	case c == '=':
		l.pos++
		return token{kind: tOp, text: "=", pos: start}, nil
	case c == '~':
		l.pos++
		return token{kind: tOp, text: "~", pos: start}, nil
	case c == '!':
		return l.lexBang()
	case c == '<':
		return l.lexRelational('<'), nil
	case c == '>':
		return l.lexRelational('>'), nil
	case c == '-' || isDigit(c):
		return l.lexNumber()
	case isIdentStart(c):
		return l.lexIdent(), nil
	default:
		return token{}, fmt.Errorf("puppetdb: lex: unexpected character %q at offset %d", string(c), start)
	}
}

// lexBang scans '!=' or '!~'.
func (l *lexer) lexBang() (token, error) {
	start := l.pos
	l.pos++
	if l.pos < len(l.src) && (l.src[l.pos] == '=' || l.src[l.pos] == '~') {
		op := "!" + string(l.src[l.pos])
		l.pos++
		return token{kind: tOp, text: op, pos: start}, nil
	}
	return token{}, fmt.Errorf("puppetdb: lex: expected '=' or '~' after '!' at offset %d", start)
}

// lexRelational scans '<'/'<=' or '>'/'>='.
func (l *lexer) lexRelational(c byte) token {
	start := l.pos
	l.pos++
	if l.pos < len(l.src) && l.src[l.pos] == '=' {
		l.pos++
		return token{kind: tOp, text: string(c) + "=", pos: start}
	}
	return token{kind: tOp, text: string(c), pos: start}
}

// lexIdent scans an identifier.
func (l *lexer) lexIdent() token {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	return token{kind: tIdent, text: l.src[start:l.pos], pos: start}
}

// lexNumber scans an integer or floating-point literal, with an optional
// leading '-' sign.
func (l *lexer) lexNumber() (token, error) {
	start := l.pos
	if l.src[l.pos] == '-' {
		l.pos++
		if l.pos >= len(l.src) || !isDigit(l.src[l.pos]) {
			return token{}, fmt.Errorf("puppetdb: lex: expected digit after '-' at offset %d", start)
		}
	}
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		l.pos++
		if l.pos >= len(l.src) || !isDigit(l.src[l.pos]) {
			return token{}, fmt.Errorf("puppetdb: lex: malformed number at offset %d", start)
		}
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	return token{kind: tNumber, text: l.src[start:l.pos], pos: start}, nil
}

// lexString scans a double-quoted string literal, honouring the escapes
// \" \\ \n \t \r; any other escaped byte is kept verbatim.
func (l *lexer) lexString() (token, error) {
	start := l.pos
	l.pos++ // opening quote
	var b strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch c {
		case '"':
			l.pos++
			return token{kind: tString, text: b.String(), pos: start}, nil
		case '\\':
			l.pos++
			if l.pos >= len(l.src) {
				return token{}, fmt.Errorf("puppetdb: lex: unterminated string at offset %d", start)
			}
			switch l.src[l.pos] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte(l.src[l.pos])
			}
			l.pos++
		default:
			b.WriteByte(c)
			l.pos++
		}
	}
	return token{}, fmt.Errorf("puppetdb: lex: unterminated string at offset %d", start)
}
