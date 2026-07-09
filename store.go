// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"fmt"
	"regexp"
	"sort"
)

// Row is a single record: a map from column name to value. Values follow JSON
// conventions (string, float64, bool, nil, or nested map[string]any).
type Row map[string]any

// Store is an in-memory dataset of rows keyed by entity, against which PQL
// queries can be evaluated without a PuppetDB server.
type Store struct {
	entities map[string][]Row
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{entities: map[string][]Row{}}
}

// Add appends rows to the named entity's dataset.
func (s *Store) Add(entity string, rows ...Row) {
	s.entities[entity] = append(s.entities[entity], rows...)
}

// Query parses a PQL string and evaluates it against the store.
func (s *Store) Query(pql string) ([]Row, error) {
	q, err := Parse(pql)
	if err != nil {
		return nil, err
	}
	return s.Eval(q)
}

// Eval evaluates a parsed query against the store, applying the filter, then the
// order-by / offset / limit modifiers, then the projection.
func (s *Store) Eval(q *Query) ([]Row, error) {
	var matched []Row
	for _, row := range s.entities[q.Entity] {
		ok, err := matchFilter(s, q.Filter, row)
		if err != nil {
			return nil, err
		}
		if ok {
			matched = append(matched, row)
		}
	}

	if len(q.OrderBy) > 0 {
		orderRows(matched, q.OrderBy)
	}
	matched = applyPaging(matched, q.Offset, q.Limit)
	return applyProjection(matched, q.Projection), nil
}

// matchFilter reports whether row satisfies expr; a nil expr matches everything.
func matchFilter(s *Store, expr Expr, row Row) (bool, error) {
	if expr == nil {
		return true, nil
	}
	return expr.evalMatch(s, row)
}

// orderRows stably sorts rows by the given order terms.
func orderRows(rows []Row, terms []OrderTerm) {
	sort.SliceStable(rows, func(i, j int) bool {
		for _, t := range terms {
			c := compareAny(rows[i][t.Field], rows[j][t.Field])
			if c == 0 {
				continue
			}
			if t.Desc {
				return c > 0
			}
			return c < 0
		}
		return false
	})
}

// applyPaging applies the offset and limit modifiers.
func applyPaging(rows []Row, offset, limit *int) []Row {
	if offset != nil {
		if *offset >= len(rows) {
			return nil
		}
		rows = rows[*offset:]
	}
	if limit != nil && *limit < len(rows) {
		rows = rows[:*limit]
	}
	return rows
}

// applyProjection reduces each row to the projected fields; an empty projection
// returns the rows unchanged.
func applyProjection(rows []Row, projection []string) []Row {
	if len(projection) == 0 {
		return rows
	}
	out := make([]Row, len(rows))
	for i, row := range rows {
		pr := Row{}
		for _, f := range projection {
			v, _ := digField(row, f)
			pr[f] = v
		}
		out[i] = pr
	}
	return out
}

// digField resolves a dotted field path within a row, returning the value and
// whether it was found.
func digField(row Row, field string) (any, bool) {
	parts := splitDots(field)
	var cur any = map[string]any(row)
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// splitDots splits a dotted field path into its segments.
func splitDots(field string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(field); i++ {
		if field[i] == '.' {
			parts = append(parts, field[start:i])
			start = i + 1
		}
	}
	parts = append(parts, field[start:])
	return parts
}

// evalMatch evaluates a comparison against a row.
func (c Comparison) evalMatch(_ *Store, row Row) (bool, error) {
	fv, found := digField(row, c.Field)
	switch c.Op {
	case "=":
		return found && valueEqual(fv, c.Value.asAny()), nil
	case "!=":
		return !(found && valueEqual(fv, c.Value.asAny())), nil
	case "~", "!~":
		return matchRegexp(c.Op, fv, found, c.Value)
	default: // < > <= >=
		return matchOrder(c.Op, fv, found, c.Value), nil
	}
}

// matchRegexp evaluates the ~ / !~ operators.
func matchRegexp(op string, fv any, found bool, lit Literal) (bool, error) {
	re, err := regexp.Compile(lit.Str)
	if err != nil {
		return false, fmt.Errorf("puppetdb: eval: invalid regexp %q: %w", lit.Str, err)
	}
	s, ok := fv.(string)
	matched := found && ok && re.MatchString(s)
	if op == "!~" {
		return !matched, nil
	}
	return matched, nil
}

// matchOrder evaluates the < > <= >= operators against numeric or string values.
func matchOrder(op string, fv any, found bool, lit Literal) bool {
	if !found {
		return false
	}
	c, ok := orderedCompare(fv, lit.asAny())
	if !ok {
		return false
	}
	switch op {
	case "<":
		return c < 0
	case ">":
		return c > 0
	case "<=":
		return c <= 0
	default: // >=
		return c >= 0
	}
}

// evalMatch evaluates a boolean composition against a row.
func (l Logical) evalMatch(s *Store, row Row) (bool, error) {
	left, err := l.Left.evalMatch(s, row)
	if err != nil {
		return false, err
	}
	if l.Op == "and" {
		if !left {
			return false, nil
		}
		return l.Right.evalMatch(s, row)
	}
	// or
	if left {
		return true, nil
	}
	return l.Right.evalMatch(s, row)
}

// evalMatch evaluates a negation against a row.
func (n Not) evalMatch(s *Store, row Row) (bool, error) {
	v, err := n.Expr.evalMatch(s, row)
	if err != nil {
		return false, err
	}
	return !v, nil
}

// evalMatch evaluates a null test against a row.
func (n IsNull) evalMatch(_ *Store, row Row) (bool, error) {
	v, found := digField(row, n.Field)
	isNull := !found || v == nil
	if n.Negate {
		return !isNull, nil
	}
	return isNull, nil
}

// evalMatch evaluates a membership test against a row, resolving the right-hand
// side from either the array literal or the subquery's projection.
func (in In) evalMatch(s *Store, row Row) (bool, error) {
	lhs := make([]any, len(in.Fields))
	for i, f := range in.Fields {
		v, _ := digField(row, f)
		lhs[i] = v
	}

	if in.Sub != nil {
		return in.evalSubquery(s, lhs)
	}
	// Array form: only meaningful for a single field.
	if len(in.Fields) != 1 {
		return false, fmt.Errorf("puppetdb: eval: array membership needs exactly one field, got %d", len(in.Fields))
	}
	for _, l := range in.Array {
		if valueEqual(lhs[0], l.asAny()) {
			return true, nil
		}
	}
	return false, nil
}

// evalSubquery evaluates the subquery on the right of an in operator and tests
// the left-hand tuple for membership in the projected rows.
func (in In) evalSubquery(s *Store, lhs []any) (bool, error) {
	rows, err := s.Eval(in.Sub)
	if err != nil {
		return false, err
	}
	if len(in.Fields) != len(in.Sub.Projection) {
		return false, fmt.Errorf("puppetdb: eval: in arity mismatch: %d field(s) vs %d projected column(s)", len(in.Fields), len(in.Sub.Projection))
	}
	for _, r := range rows {
		if tupleEqual(lhs, in.Sub.Projection, r) {
			return true, nil
		}
	}
	return false, nil
}

// tupleEqual reports whether the left-hand values equal the projected columns of
// row r, position by position.
func tupleEqual(lhs []any, cols []string, r Row) bool {
	for i, col := range cols {
		if !valueEqual(lhs[i], r[col]) {
			return false
		}
	}
	return true
}
