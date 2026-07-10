// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

// Query is a parsed PQL query: an entity, an optional projection (extract)
// field list, an optional filter expression, an optional group-by clause and
// the optional paging modifiers.
type Query struct {
	// Entity is the queried PuppetDB entity (nodes, resources, facts, ...).
	Entity string
	// Projection is the extract list; empty means "all columns". Each item is
	// either a plain (dotted) field or an aggregate/transform function.
	Projection []ProjItem
	// Filter is the where-clause expression; nil means "match everything".
	Filter Expr
	// GroupBy holds the group-by items (fields or functions); empty means no
	// grouping.
	GroupBy []ProjItem
	// OrderBy holds the order-by terms, in significance order.
	OrderBy []OrderTerm
	// Limit, when non-nil, caps the number of returned rows.
	Limit *int
	// Offset, when non-nil, skips that many leading rows.
	Offset *int
}

// ProjItem is one item of an extract or group-by list: either a plain dotted
// field (Field non-empty, Func nil) or a function call (Func non-nil).
type ProjItem struct {
	Field string
	Func  *Func
}

// Func is an aggregate or transform function appearing in a projection or
// group-by list. Name is one of count, avg, sum, min, max, to_string. Args are
// its arguments (a leading field for the aggregates; a field plus format
// strings for to_string); count() has no arguments.
type Func struct {
	Name string
	Args []FuncArg
}

// FuncArg is a single function argument: either a field reference (Str false)
// or a string literal (Str true).
type FuncArg struct {
	Text string
	Str  bool
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

// RegexpArray is the ~> operator: the field (an array value) matches when each
// of its elements matches the corresponding regexp in Patterns, position by
// position.
type RegexpArray struct {
	Field    string
	Patterns []string
}

func (RegexpArray) isExpr() {}

// Subquery is an implicit subquery: a bare "entity { filter }" appearing inside
// an expression. It compiles to ["subquery", entity, filter]. A nil Filter
// compiles to ["subquery", entity].
type Subquery struct {
	Entity string
	Filter Expr
}

func (Subquery) isExpr() {}

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
