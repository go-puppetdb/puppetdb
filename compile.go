// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"bytes"
	"encoding/json"
)

// AST compiles the query into PuppetDB's canonical AST-query value, ready to be
// JSON-encoded. The shape is ["from", <entity>, <inner>, <modifiers>...]. When a
// projection is present the <inner> is an ["extract", <cols>, <filter?>,
// <group_by?>] node (the group_by, per PuppetDB, is the last argument of an
// extract); otherwise the bare filter and/or group_by clauses appear directly.
func (q *Query) AST() any {
	out := []any{"from", q.Entity}

	filter := compileExpr(q.Filter)
	groupBy := compileGroupBy(q.GroupBy)
	if len(q.Projection) > 0 {
		extract := []any{"extract", compileProjList(q.Projection)}
		if filter != nil {
			extract = append(extract, filter)
		}
		if groupBy != nil {
			extract = append(extract, groupBy)
		}
		out = append(out, extract)
	} else {
		if filter != nil {
			out = append(out, filter)
		}
		if groupBy != nil {
			out = append(out, groupBy)
		}
	}

	if len(q.OrderBy) > 0 {
		terms := make([]any, 0, len(q.OrderBy))
		for _, t := range q.OrderBy {
			dir := "asc"
			if t.Desc {
				dir = "desc"
			}
			terms = append(terms, []any{t.Field, dir})
		}
		out = append(out, []any{"order_by", terms})
	}
	if q.Limit != nil {
		out = append(out, []any{"limit", *q.Limit})
	}
	if q.Offset != nil {
		out = append(out, []any{"offset", *q.Offset})
	}
	return out
}

// compileProjList compiles an extract list to its JSON columns array, mapping
// plain fields to strings and functions to ["function", name, args...] nodes.
func compileProjList(items []ProjItem) []any {
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = it.compile()
	}
	return out
}

// compileGroupBy compiles a group-by list to a ["group_by", item...] node, or
// nil when there is no grouping.
func compileGroupBy(items []ProjItem) any {
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items)+1)
	out = append(out, "group_by")
	for _, it := range items {
		out = append(out, it.compile())
	}
	return out
}

// compile maps a projection item to its JSON form: a bare field string or a
// function node.
func (it ProjItem) compile() any {
	if it.Func != nil {
		return it.Func.compile()
	}
	return it.Field
}

// compile maps a function to ["function", name, arg...]; each argument (a field
// or a string literal) is emitted as a JSON string.
func (f *Func) compile() any {
	out := make([]any, 0, len(f.Args)+2)
	out = append(out, "function", f.Name)
	for _, a := range f.Args {
		out = append(out, a.Text)
	}
	return out
}

// MarshalAST compiles the query and JSON-encodes it. The encoding of the
// compiled value never fails, so no error is returned.
func (q *Query) MarshalAST() []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(q.AST()) // encoding the compiled value never fails
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// compileExpr compiles a filter expression, mapping a nil expression (an absent
// filter) to a nil value.
func compileExpr(e Expr) any {
	if e == nil {
		return nil
	}
	return e.compileAST()
}

// compileAST maps a comparison to canonical form; != and !~ become a negated
// = / ~ node, as PuppetDB has no direct inequality operators.
func (c Comparison) compileAST() any {
	val := c.Value.asAny()
	switch c.Op {
	case "!=":
		return []any{"not", []any{"=", c.Field, val}}
	case "!~":
		return []any{"not", []any{"~", c.Field, val}}
	default: // = ~ < > <= >=
		return []any{c.Op, c.Field, val}
	}
}

// compileAST maps a boolean composition to ["and"/"or", left, right].
func (l Logical) compileAST() any {
	return []any{l.Op, l.Left.compileAST(), l.Right.compileAST()}
}

// compileAST maps a negation to ["not", inner].
func (n Not) compileAST() any {
	return []any{"not", n.Expr.compileAST()}
}

// compileAST maps a null test to ["null?", field, <isNull>].
func (n IsNull) compileAST() any {
	return []any{"null?", n.Field, !n.Negate}
}

// compileAST maps a regexp-array test to ["~>", field, [pattern, ...]].
func (r RegexpArray) compileAST() any {
	pats := make([]any, len(r.Patterns))
	for i, p := range r.Patterns {
		pats[i] = p
	}
	return []any{"~>", r.Field, pats}
}

// compileAST maps an implicit subquery to ["subquery", entity, filter], or
// ["subquery", entity] when the subquery has no filter.
func (sq Subquery) compileAST() any {
	if sq.Filter == nil {
		return []any{"subquery", sq.Entity}
	}
	return []any{"subquery", sq.Entity, sq.Filter.compileAST()}
}

// compileAST maps a membership test to canonical form. A single field compiles
// to a scalar; multiple fields to an array. The array form uses ["array", ...];
// the subquery form nests the subquery's own ["from", ...] AST.
func (in In) compileAST() any {
	var lhs any
	if len(in.Fields) == 1 {
		lhs = in.Fields[0]
	} else {
		fields := make([]any, len(in.Fields))
		for i, f := range in.Fields {
			fields[i] = f
		}
		lhs = fields
	}
	if in.Sub != nil {
		return []any{"in", lhs, in.Sub.AST()}
	}
	vals := make([]any, len(in.Array))
	for i, l := range in.Array {
		vals[i] = l.asAny()
	}
	return []any{"in", lhs, []any{"array", vals}}
}
