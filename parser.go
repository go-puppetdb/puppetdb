// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"fmt"
	"strconv"
	"strings"
)

// entities is the set of PQL entities this package understands. It mirrors the
// entity list of PuppetDB's PQL grammar.
var entities = map[string]bool{
	"nodes":             true,
	"resources":         true,
	"facts":             true,
	"inventory":         true,
	"catalogs":          true,
	"reports":           true,
	"events":            true,
	"edges":             true,
	"fact_contents":     true,
	"fact_paths":        true,
	"factsets":          true,
	"environments":      true,
	"packages":          true,
	"package_inventory": true,
}

// funcNames is the set of aggregate/transform function names PQL supports in
// extract and group-by lists.
var funcNames = map[string]bool{
	"count":     true,
	"avg":       true,
	"sum":       true,
	"min":       true,
	"max":       true,
	"to_string": true,
}

// entityName resolves an identifier to an entity name, accepting both the plain
// spelling ("resources") and the legacy select_ subquery spelling
// ("select_resources"). It reports whether the identifier names an entity.
func entityName(text string) (string, bool) {
	if entities[text] {
		return text, true
	}
	if e, ok := strings.CutPrefix(text, "select_"); ok && entities[e] {
		return e, true
	}
	return "", false
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

// parseQuery parses "entity [projection] { [filter] [group by / paging] }
// [group by / paging]". The group-by and paging clauses are accepted both
// inside the braces (as in PuppetDB's PQL grammar) and, as a superset, after
// them.
func (p *parser) parseQuery() (*Query, error) {
	ent, err := p.expect(tIdent, "entity name")
	if err != nil {
		return nil, err
	}
	name, ok := entityName(ent.text)
	if !ok {
		return nil, p.errorf("unknown entity %q", ent.text)
	}
	q := &Query{Entity: name}

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
	if err := p.parseWhereBody(q); err != nil {
		return nil, err
	}
	if _, err := p.expect(tRBrace, "'}'"); err != nil {
		return nil, err
	}

	if err := p.parseClauses(q); err != nil {
		return nil, err
	}
	return q, nil
}

// parseWhereBody parses the inside of the braces: an optional filter expression
// followed by optional group-by / paging clauses, stopping at the closing brace.
func (p *parser) parseWhereBody(q *Query) error {
	if p.peek().kind == tRBrace {
		return nil
	}
	if !p.atClauseStart() {
		filter, err := p.parseExpr()
		if err != nil {
			return err
		}
		q.Filter = filter
	}
	return p.parseClauses(q)
}

// atClauseStart reports whether the current token begins a group-by or paging
// clause (rather than a filter expression).
func (p *parser) atClauseStart() bool {
	t := p.peek()
	if t.kind != tIdent {
		return false
	}
	switch t.text {
	case "group", "order", "limit", "offset":
		return true
	default:
		return false
	}
}

// parseClauses consumes zero or more group-by / order-by / limit / offset
// clauses in any order, stopping at the first token that starts none of them.
func (p *parser) parseClauses(q *Query) error {
	for {
		t := p.peek()
		if t.kind != tIdent {
			return nil
		}
		var err error
		switch t.text {
		case "order":
			err = p.parseOrderBy(q)
		case "group":
			err = p.parseGroupBy(q)
		case "limit":
			p.advance()
			n, e := p.parseNonNegInt()
			q.Limit, err = &n, e
		case "offset":
			p.advance()
			n, e := p.parseNonNegInt()
			q.Offset, err = &n, e
		default:
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// parseGroupBy parses "group by field-or-function (, field-or-function)*".
func (p *parser) parseGroupBy(q *Query) error {
	p.advance() // 'group'
	if err := p.expectKeyword("by"); err != nil {
		return err
	}
	items, err := p.parseProjList()
	if err != nil {
		return err
	}
	q.GroupBy = items
	return nil
}

// parseProjection parses "[ field-or-function, ... ]" (possibly empty).
func (p *parser) parseProjection() ([]ProjItem, error) {
	p.advance() // '[' (caller has verified it is present)
	if p.peek().kind == tRBracket {
		p.advance()
		return nil, nil
	}
	items, err := p.parseProjList()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tRBracket, "']'"); err != nil {
		return nil, err
	}
	return items, nil
}

// parseProjList parses a comma-separated list of one or more projection items
// (fields or functions).
func (p *parser) parseProjList() ([]ProjItem, error) {
	var items []ProjItem
	for {
		it, err := p.parseProjItem()
		if err != nil {
			return nil, err
		}
		items = append(items, it)
		if p.peek().kind != tComma {
			break
		}
		p.advance()
	}
	return items, nil
}

// parseProjItem parses a single projection item: a function call when the
// current identifier is a function name immediately followed by '(', otherwise a
// plain dotted field.
func (p *parser) parseProjItem() (ProjItem, error) {
	t := p.peek()
	if t.kind == tIdent && funcNames[t.text] && p.toks[p.pos+1].kind == tLParen {
		fn, err := p.parseFunc()
		if err != nil {
			return ProjItem{}, err
		}
		return ProjItem{Func: fn}, nil
	}
	f, err := p.parseField()
	if err != nil {
		return ProjItem{}, err
	}
	return ProjItem{Field: f}, nil
}

// parseFunc parses "name ( [arg (, arg)*] )". The caller has verified that the
// function name is immediately followed by '(', so both are consumed directly.
func (p *parser) parseFunc() (*Func, error) {
	name := p.advance().text // function name
	p.advance()              // '('
	fn := &Func{Name: name}
	if p.peek().kind != tRParen {
		for {
			arg, err := p.parseFuncArg()
			if err != nil {
				return nil, err
			}
			fn.Args = append(fn.Args, arg)
			if p.peek().kind != tComma {
				break
			}
			p.advance()
		}
	}
	if _, err := p.expect(tRParen, "')'"); err != nil {
		return nil, err
	}
	return fn, nil
}

// parseFuncArg parses a single function argument: a string literal or a field.
func (p *parser) parseFuncArg() (FuncArg, error) {
	if t := p.peek(); t.kind == tString {
		p.advance()
		return FuncArg{Text: t.text, Str: true}, nil
	}
	f, err := p.parseField()
	if err != nil {
		return FuncArg{}, err
	}
	return FuncArg{Text: f}, nil
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
		// A bare "entity { ... }" is an implicit subquery.
		if ent, ok := entityName(t.text); ok && p.toks[p.pos+1].kind == tLBrace {
			return p.parseImplicitSubquery(ent)
		}
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

// parseImplicitSubquery parses "entity { [filter] }" for an already-recognised
// entity, producing a [Subquery] node. The caller has verified the entity is
// immediately followed by '{', so both are consumed directly.
func (p *parser) parseImplicitSubquery(entity string) (Expr, error) {
	p.advance() // entity identifier
	p.advance() // '{'
	var filter Expr
	if p.peek().kind != tRBrace {
		f, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		filter = f
	}
	if _, err := p.expect(tRBrace, "'}'"); err != nil {
		return nil, err
	}
	return Subquery{Entity: entity, Filter: filter}, nil
}

// parseComparison parses "<op> <rhs>" for the given field. The rhs is a grouped
// regexp list for the ~> operator and a scalar literal for every other operator.
func (p *parser) parseComparison(field string) (Expr, error) {
	op := p.advance().text
	if op == "~>" {
		pats, err := p.parseRegexpList()
		if err != nil {
			return nil, err
		}
		return RegexpArray{Field: field, Patterns: pats}, nil
	}
	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return Comparison{Op: op, Field: field, Value: lit}, nil
}

// parseRegexpList parses "[ string (, string)* ]" for the ~> operator.
func (p *parser) parseRegexpList() ([]string, error) {
	if _, err := p.expect(tLBracket, "'[' after '~>'"); err != nil {
		return nil, err
	}
	var pats []string
	for {
		t, err := p.expect(tString, "regexp string")
		if err != nil {
			return nil, err
		}
		pats = append(pats, t.text)
		if p.peek().kind != tComma {
			break
		}
		p.advance()
	}
	if _, err := p.expect(tRBracket, "']'"); err != nil {
		return nil, err
	}
	return pats, nil
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
