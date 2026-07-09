// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"bytes"
	"encoding/json"
)

// AST compiles the query into PuppetDB's canonical AST-query value, ready to be
// JSON-encoded. The shape is ["from", <entity>, <inner>, <modifiers>...] where
// <inner> is an ["extract", ...] node when a projection is present, otherwise
// the bare filter clause (omitted entirely when there is no filter).
func (q *Query) AST() any {
	out := []any{"from", q.Entity}

	if inner := compileInner(q); inner != nil {
		out = append(out, inner)
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

// compileInner builds the extract-or-filter node of a query, or nil when the
// query has neither a projection nor a filter.
func compileInner(q *Query) any {
	filter := compileExpr(q.Filter)
	if len(q.Projection) > 0 {
		proj := make([]any, len(q.Projection))
		for i, f := range q.Projection {
			proj[i] = f
		}
		if filter == nil {
			return []any{"extract", proj}
		}
		return []any{"extract", proj, filter}
	}
	return filter
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
