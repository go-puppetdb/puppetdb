// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"fmt"
	"strconv"
)

// entities is the set of PQL entities this package understands.
var entities = map[string]bool{
	"nodes":         true,
	"resources":     true,
	"facts":         true,
	"inventory":     true,
	"catalogs":      true,
	"reports":       true,
	"events":        true,
	"edges":         true,
	"fact_contents": true,
}

// parser is a recursive-descent PQL parser over a token slice.
type parser struct {
	toks []token
	pos  int
}

// Parse lexes and parses a PQL query string into a typed [Query].
func Parse(src string) (*Query, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	q, err := p.parseQuery()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, p.errorf("unexpected trailing token %q", t.text)
	}
	return q, nil
}

// peek returns the current token without consuming it.
func (p *parser) peek() token { return p.toks[p.pos] }

// advance consumes and returns the current token.
func (p *parser) advance() token {
	t := p.toks[p.pos]
	if t.kind != tEOF {
		p.pos++
	}
	return t
}

// errorf builds a parse error anchored at the current token offset.
func (p *parser) errorf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("puppetdb: parse: %s at offset %d", msg, p.peek().pos)
}

// expect consumes the current token if it matches kind, else errors.
func (p *parser) expect(kind tokenKind, what string) (token, error) {
	t := p.peek()
	if t.kind != kind {
		return token{}, p.errorf("expected %s", what)
	}
	return p.advance(), nil
}

// expectKeyword consumes an identifier token equal to word, else errors.
func (p *parser) expectKeyword(word string) error {
	t := p.peek()
	if t.kind != tIdent || t.text != word {
		return p.errorf("expected keyword %q", word)
	}
	p.advance()
	return nil
}

// parseQuery parses "entity [projection] { [filter] } modifiers".
func (p *parser) parseQuery() (*Query, error) {
	ent, err := p.expect(tIdent, "entity name")
	if err != nil {
		return nil, err
	}
	if !entities[ent.text] {
		return nil, p.errorf("unknown entity %q", ent.text)
	}
	q := &Query{Entity: ent.text}

	if p.peek().kind == tLBracket {
		proj, err := p.parseProjection()
		if err != nil {
			return nil, err
		}
		q.Projection = proj
	}

	if _, err := p.expect(tLBrace, "'{'"); err != nil {
		return nil, err
	}
	if p.peek().kind != tRBrace {
		filter, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		q.Filter = filter
	}
	if _, err := p.expect(tRBrace, "'}'"); err != nil {
		return nil, err
	}

	if err := p.parseModifiers(q); err != nil {
		return nil, err
	}
	return q, nil
}

// parseProjection parses "[ field, field, ... ]" (possibly empty).
func (p *parser) parseProjection() ([]string, error) {
	p.advance() // '[' (caller has verified it is present)
	var fields []string
	if p.peek().kind != tRBracket {
		for {
			f, err := p.parseField()
			if err != nil {
				return nil, err
			}
			fields = append(fields, f)
			if p.peek().kind != tComma {
				break
			}
			p.advance()
		}
	}
	if _, err := p.expect(tRBracket, "']'"); err != nil {
		return nil, err
	}
	return fields, nil
}

// parseField parses a dotted field path "a.b.c".
func (p *parser) parseField() (string, error) {
	t, err := p.expect(tIdent, "field name")
	if err != nil {
		return "", err
	}
	name := t.text
	for p.peek().kind == tDot {
		p.advance()
		part, err := p.expect(tIdent, "field name after '.'")
		if err != nil {
			return "", err
		}
		name += "." + part.text
	}
	return name, nil
}

// parseExpr parses a full filter expression (lowest precedence: or).
func (p *parser) parseExpr() (Expr, error) { return p.parseOr() }

// parseOr parses "and-expr (or and-expr)*".
func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tIdent && p.peek().text == "or" {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = Logical{Op: "or", Left: left, Right: right}
	}
	return left, nil
}

// parseAnd parses "not-expr (and not-expr)*".
func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tIdent && p.peek().text == "and" {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = Logical{Op: "and", Left: left, Right: right}
	}
	return left, nil
}

// parseNot parses "not not-expr" or a primary.
func (p *parser) parseNot() (Expr, error) {
	if p.peek().kind == tIdent && p.peek().text == "not" {
		p.advance()
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return Not{Expr: inner}, nil
	}
	return p.parsePrimary()
}

// parsePrimary parses a parenthesised expression, a field-list membership, or a
// field-led comparison / membership / is-null test.
func (p *parser) parsePrimary() (Expr, error) {
	t := p.peek()
	switch t.kind {
	case tLParen:
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen, "')'"); err != nil {
			return nil, err
		}
		return e, nil
	case tLBracket:
		fields, err := p.parseFieldList()
		if err != nil {
			return nil, err
		}
		return p.parseIn(fields)
	case tIdent:
		field, err := p.parseField()
		if err != nil {
			return nil, err
		}
		next := p.peek()
		switch {
		case next.kind == tOp:
			return p.parseComparison(field)
		case next.kind == tIdent && next.text == "in":
			return p.parseIn([]string{field})
		case next.kind == tIdent && next.text == "is":
			return p.parseIsNull(field)
		default:
			return nil, p.errorf("expected operator, 'in' or 'is' after field %q", field)
		}
	default:
		return nil, p.errorf("unexpected token %q", t.text)
	}
}

// parseComparison parses "<op> <literal>" for the given field.
func (p *parser) parseComparison(field string) (Expr, error) {
	op := p.advance().text
	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return Comparison{Op: op, Field: field, Value: lit}, nil
}

// parseIsNull parses "is [not] null" for the given field.
func (p *parser) parseIsNull(field string) (Expr, error) {
	p.advance() // 'is'
	negate := false
	if p.peek().kind == tIdent && p.peek().text == "not" {
		p.advance()
		negate = true
	}
	if err := p.expectKeyword("null"); err != nil {
		return nil, err
	}
	return IsNull{Field: field, Negate: negate}, nil
}

// parseIn parses "in ( array-literal | subquery )" for the given fields.
func (p *parser) parseIn(fields []string) (Expr, error) {
	if err := p.expectKeyword("in"); err != nil {
		return nil, err
	}
	switch p.peek().kind {
	case tLBracket:
		arr, err := p.parseArrayLiteral()
		if err != nil {
			return nil, err
		}
		return In{Fields: fields, Array: arr}, nil
	case tIdent:
		sub, err := p.parseQuery()
		if err != nil {
			return nil, err
		}
		if len(sub.Projection) == 0 {
			return nil, p.errorf("subquery on the right of 'in' requires a projection")
		}
		return In{Fields: fields, Sub: sub}, nil
	default:
		return nil, p.errorf("expected array literal or subquery after 'in'")
	}
}

// parseFieldList parses "[ field, field, ... ]" requiring at least one field.
func (p *parser) parseFieldList() ([]string, error) {
	p.advance() // '['
	if p.peek().kind == tRBracket {
		return nil, p.errorf("empty field list")
	}
	var fields []string
	for {
		f, err := p.parseField()
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
		if p.peek().kind != tComma {
			break
		}
		p.advance()
	}
	if _, err := p.expect(tRBracket, "']'"); err != nil {
		return nil, err
	}
	return fields, nil
}

// parseArrayLiteral parses "[ literal, literal, ... ]" (possibly empty).
func (p *parser) parseArrayLiteral() ([]Literal, error) {
	p.advance() // '['
	vals := []Literal{}
	if p.peek().kind == tRBracket {
		p.advance()
		return vals, nil
	}
	for {
		v, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
		if p.peek().kind != tComma {
			break
		}
		p.advance()
	}
	if _, err := p.expect(tRBracket, "']'"); err != nil {
		return nil, err
	}
	return vals, nil
}

// parseLiteral parses a string, number, boolean or null literal.
func (p *parser) parseLiteral() (Literal, error) {
	t := p.advance()
	switch t.kind {
	case tString:
		return Literal{Kind: LitString, Str: t.text}, nil
	case tNumber:
		f, _ := strconv.ParseFloat(t.text, 64) // text is lexer-validated
		return Literal{Kind: LitNumber, Num: f}, nil
	case tIdent:
		switch t.text {
		case "true":
			return Literal{Kind: LitBool, Bool: true}, nil
		case "false":
			return Literal{Kind: LitBool, Bool: false}, nil
		case "null":
			return Literal{Kind: LitNull}, nil
		default:
			return Literal{}, p.errorf("expected literal, got identifier %q", t.text)
		}
	default:
		return Literal{}, p.errorf("expected a literal value")
	}
}

// parseModifiers parses zero or more of the order-by / limit / offset trailers.
func (p *parser) parseModifiers(q *Query) error {
loop:
	for {
		t := p.peek()
		if t.kind != tIdent {
			break
		}
		switch t.text {
		case "order":
			if err := p.parseOrderBy(q); err != nil {
				return err
			}
		case "limit":
			p.advance()
			n, err := p.parseNonNegInt()
			if err != nil {
				return err
			}
			q.Limit = &n
		case "offset":
			p.advance()
			n, err := p.parseNonNegInt()
			if err != nil {
				return err
			}
			q.Offset = &n
		default:
			break loop
		}
	}
	return nil
}

// parseOrderBy parses "order by field [asc|desc] (, field [asc|desc])*".
func (p *parser) parseOrderBy(q *Query) error {
	p.advance() // 'order'
	if err := p.expectKeyword("by"); err != nil {
		return err
	}
	for {
		field, err := p.parseField()
		if err != nil {
			return err
		}
		term := OrderTerm{Field: field}
		if n := p.peek(); n.kind == tIdent && (n.text == "asc" || n.text == "desc") {
			term.Desc = n.text == "desc"
			p.advance()
		}
		q.OrderBy = append(q.OrderBy, term)
		if p.peek().kind != tComma {
			break
		}
		p.advance()
	}
	return nil
}

// parseNonNegInt parses a non-negative integer literal (for limit / offset).
func (p *parser) parseNonNegInt() (int, error) {
	t := p.peek()
	if t.kind != tNumber {
		return 0, p.errorf("expected an integer")
	}
	p.advance()
	n, err := strconv.Atoi(t.text)
	if err != nil {
		return 0, p.errorf("expected an integer, got %q", t.text)
	}
	if n < 0 {
		return 0, p.errorf("expected a non-negative integer, got %d", n)
	}
	return n, nil
}
