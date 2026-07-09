// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

// Query is a parsed PQL query: an entity, an optional projection (extract)
// field list, an optional filter expression and the optional paging modifiers.
type Query struct {
	// Entity is the queried PuppetDB entity (nodes, resources, facts, ...).
	Entity string
	// Projection is the extract field list; empty means "all columns".
	Projection []string
	// Filter is the where-clause expression; nil means "match everything".
	Filter Expr
	// OrderBy holds the order-by terms, in significance order.
	OrderBy []OrderTerm
	// Limit, when non-nil, caps the number of returned rows.
	Limit *int
	// Offset, when non-nil, skips that many leading rows.
	Offset *int
}

// OrderTerm is a single order-by column and its direction.
type OrderTerm struct {
	Field string
	Desc  bool
}

// Expr is the interface implemented by every PQL filter node. Beyond the
// marker method it carries the two operations every node supports: compilation
// to canonical AST-query JSON and evaluation against an in-memory row.
type Expr interface {
	isExpr()
	compileAST() any
	evalMatch(s *Store, row Row) (bool, error)
}

// Comparison is a binary comparison between a field and a literal. Op is one of
// "=", "!=", "<", ">", "<=", ">=", "~" (regexp match) or "!~" (regexp
// non-match).
type Comparison struct {
	Op    string
	Field string
	Value Literal
}

func (Comparison) isExpr() {}

// Logical is a boolean composition of two sub-expressions. Op is "and" or "or".
type Logical struct {
	Op    string
	Left  Expr
	Right Expr
}

func (Logical) isExpr() {}

// Not negates a sub-expression.
type Not struct{ Expr Expr }

func (Not) isExpr() {}

// IsNull tests a field for null (Negate reports the "is not null" form).
type IsNull struct {
	Field  string
	Negate bool
}

func (IsNull) isExpr() {}

// In is a membership test: the tuple of Fields must appear either in the array
// of literal values (Array) or in the projection of the subquery (Sub). Exactly
// one of Array/Sub is set.
type In struct {
	Fields []string
	Array  []Literal
	Sub    *Query
}

func (In) isExpr() {}

// LitKind enumerates the literal value kinds.
type LitKind int

const (
	// LitString is a double-quoted string literal.
	LitString LitKind = iota
	// LitNumber is an integer or floating-point literal.
	LitNumber
	// LitBool is the true or false literal.
	LitBool
	// LitNull is the null literal.
	LitNull
)

// Literal is a typed scalar value appearing on the right of a comparison or in
// an array membership list.
type Literal struct {
	Kind LitKind
	Str  string
	Num  float64
	Bool bool
}

// asAny converts a literal to its natural Go/JSON value.
func (l Literal) asAny() any {
	switch l.Kind {
	case LitString:
		return l.Str
	case LitNumber:
		return l.Num
	case LitBool:
		return l.Bool
	default: // LitNull
		return nil
	}
}
