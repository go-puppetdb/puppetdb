// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"encoding/json"
	"fmt"
)

// ParseAST parses PuppetDB's canonical AST-query JSON — the
// ["from", entity, <inner>, <modifiers>...] wire form produced by [Query.AST]
// and accepted by the /pdb/query/v4 endpoint — back into a typed [Query].
//
// It is the inverse of [Query.AST]: for every query this package emits,
// re-compiling the parsed result reproduces the original AST byte-for-byte.
// PuppetDB's n-ary ["and"/"or", a, b, c, ...] clauses are accepted and folded
// into the left-nested binary form this package uses; the result is
// semantically identical though its re-compiled AST is re-associated.
func ParseAST(data []byte) (*Query, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("puppetdb: ast: invalid JSON: %w", err)
	}
	return queryFromAST(v)
}

// nodeHead reports whether item is a non-empty array with a string head and, if
// so, returns the head and the whole array.
func nodeHead(item any) (string, []any, bool) {
	arr, ok := item.([]any)
	if !ok || len(arr) == 0 {
		return "", nil, false
	}
	head, ok := arr[0].(string)
	if !ok {
		return "", nil, false
	}
	return head, arr, true
}

// queryFromAST converts a decoded ["from", entity, ...] value into a [Query].
func queryFromAST(v any) (*Query, error) {
	head, arr, ok := nodeHead(v)
	if !ok || len(arr) < 2 {
		return nil, fmt.Errorf("puppetdb: ast: query must be a [\"from\", entity, ...] array")
	}
	if head != "from" {
		return nil, fmt.Errorf("puppetdb: ast: query must start with \"from\"")
	}
	entity, ok := arr[1].(string)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: entity name must be a string")
	}
	if !entities[entity] {
		return nil, fmt.Errorf("puppetdb: ast: unknown entity %q", entity)
	}
	q := &Query{Entity: entity}
	if err := applyASTRest(q, arr[2:]); err != nil {
		return nil, err
	}
	return q, nil
}

// isModifierHead reports whether a node head names a trailing modifier clause.
func isModifierHead(head string) bool {
	switch head {
	case "order_by", "limit", "offset":
		return true
	default:
		return false
	}
}

// applyASTRest applies the trailing elements after the entity: at most one inner
// (extract or filter) clause, an optional group_by clause, and any order_by /
// limit / offset modifiers, in any order.
func applyASTRest(q *Query, rest []any) error {
	innerSet, groupSet := false, false
	for _, item := range rest {
		if head, arr, ok := nodeHead(item); ok {
			switch {
			case isModifierHead(head):
				if err := applyModifier(q, arr); err != nil {
					return err
				}
				continue
			case head == "group_by":
				if groupSet {
					return fmt.Errorf("puppetdb: ast: multiple group_by clauses")
				}
				items, err := projItemsFromAST(arr[1:])
				if err != nil {
					return err
				}
				q.GroupBy, groupSet = items, true
				continue
			}
		}
		if innerSet {
			return fmt.Errorf("puppetdb: ast: multiple inner clauses")
		}
		if err := applyInner(q, item); err != nil {
			return err
		}
		innerSet = true
	}
	return nil
}

// applyModifier applies a single order_by / limit / offset clause to q.
func applyModifier(q *Query, arr []any) error {
	switch arr[0].(string) {
	case "order_by":
		return applyOrderBy(q, arr)
	case "limit":
		n, err := astInt(arr, 1, "limit")
		if err != nil {
			return err
		}
		q.Limit = &n
	default: // offset
		n, err := astInt(arr, 1, "offset")
		if err != nil {
			return err
		}
		q.Offset = &n
	}
	return nil
}

// applyOrderBy parses ["order_by", [[field, dir], ...]] into q.OrderBy.
func applyOrderBy(q *Query, arr []any) error {
	if len(arr) != 2 {
		return fmt.Errorf("puppetdb: ast: order_by takes one term list")
	}
	terms, ok := arr[1].([]any)
	if !ok {
		return fmt.Errorf("puppetdb: ast: order_by terms must be an array")
	}
	for _, t := range terms {
		term, ok := t.([]any)
		if !ok || len(term) != 2 {
			return fmt.Errorf("puppetdb: ast: order_by term must be [field, direction]")
		}
		field, ok := term[0].(string)
		if !ok {
			return fmt.Errorf("puppetdb: ast: order_by field must be a string")
		}
		dir, ok := term[1].(string)
		if !ok || (dir != "asc" && dir != "desc") {
			return fmt.Errorf("puppetdb: ast: order_by direction must be \"asc\" or \"desc\"")
		}
		q.OrderBy = append(q.OrderBy, OrderTerm{Field: field, Desc: dir == "desc"})
	}
	return nil
}

// applyInner applies the extract-or-filter clause to q.
func applyInner(q *Query, node any) error {
	if head, arr, ok := nodeHead(node); ok && head == "extract" {
		return applyExtract(q, arr)
	}
	filter, err := exprFromAST(node)
	if err != nil {
		return err
	}
	q.Filter = filter
	return nil
}

// applyExtract parses ["extract", [cols], <filter>?, <group_by>?] into q. The
// column list may hold plain fields and function nodes; a trailing group_by node
// and/or a filter clause may follow in either order (PuppetDB emits group_by
// last).
func applyExtract(q *Query, arr []any) error {
	if len(arr) < 2 {
		return fmt.Errorf("puppetdb: ast: extract needs a column list")
	}
	cols, ok := arr[1].([]any)
	if !ok {
		return fmt.Errorf("puppetdb: ast: extract columns must be an array")
	}
	items, err := projItemsFromAST(cols)
	if err != nil {
		return err
	}
	q.Projection = items
	for _, extra := range arr[2:] {
		if head, sub, ok := nodeHead(extra); ok && head == "group_by" {
			gi, err := projItemsFromAST(sub[1:])
			if err != nil {
				return err
			}
			q.GroupBy = gi
			continue
		}
		if q.Filter != nil {
			return fmt.Errorf("puppetdb: ast: multiple filter clauses in extract")
		}
		filter, err := exprFromAST(extra)
		if err != nil {
			return err
		}
		q.Filter = filter
	}
	return nil
}

// projItemsFromAST converts a decoded projection/group-by list into [ProjItem]s:
// strings become plain fields, ["function", name, args...] nodes become
// functions.
func projItemsFromAST(vals []any) ([]ProjItem, error) {
	items := make([]ProjItem, 0, len(vals))
	for _, v := range vals {
		switch x := v.(type) {
		case string:
			items = append(items, ProjItem{Field: x})
		case []any:
			fn, err := funcFromAST(x)
			if err != nil {
				return nil, err
			}
			items = append(items, ProjItem{Func: fn})
		default:
			return nil, fmt.Errorf("puppetdb: ast: projection item must be a field or function")
		}
	}
	return items, nil
}

// funcFromAST parses a ["function", name, arg...] node. Because [Func.compile]
// emits both field and string-literal arguments as plain strings, parsed
// arguments are treated as fields; this preserves the compiled AST exactly.
func funcFromAST(arr []any) (*Func, error) {
	if len(arr) < 2 {
		return nil, fmt.Errorf("puppetdb: ast: function node needs a name")
	}
	head, ok := arr[0].(string)
	if !ok || head != "function" {
		return nil, fmt.Errorf("puppetdb: ast: projection function must start with \"function\"")
	}
	name, ok := arr[1].(string)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: function name must be a string")
	}
	if !funcNames[name] {
		return nil, fmt.Errorf("puppetdb: ast: unknown function %q", name)
	}
	fn := &Func{Name: name}
	for _, a := range arr[2:] {
		s, ok := a.(string)
		if !ok {
			return nil, fmt.Errorf("puppetdb: ast: function argument must be a string")
		}
		fn.Args = append(fn.Args, FuncArg{Text: s})
	}
	return fn, nil
}

// exprFromAST converts a decoded filter clause into a typed [Expr].
func exprFromAST(node any) (Expr, error) {
	head, arr, ok := nodeHead(node)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: filter clause must be a non-empty array with a string operator")
	}
	switch head {
	case "=", "~", "<", ">", "<=", ">=":
		return comparisonFromAST(head, arr)
	case "and", "or":
		return logicalFromAST(head, arr)
	case "not":
		return notFromAST(arr)
	case "null?":
		return isNullFromAST(arr)
	case "~>":
		return regexpArrayFromAST(arr)
	case "subquery":
		return subqueryFromAST(arr)
	case "in":
		return inFromAST(arr)
	default:
		return nil, fmt.Errorf("puppetdb: ast: unknown operator %q", head)
	}
}

// comparisonFromAST parses ["<op>", field, value].
func comparisonFromAST(op string, arr []any) (Expr, error) {
	if len(arr) != 3 {
		return nil, fmt.Errorf("puppetdb: ast: %q takes a field and a value", op)
	}
	field, ok := arr[1].(string)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: %q field must be a string", op)
	}
	lit, err := literalFromAny(arr[2])
	if err != nil {
		return nil, err
	}
	return Comparison{Op: op, Field: field, Value: lit}, nil
}

// logicalFromAST parses ["and"/"or", clause, clause, ...] folding an n-ary
// clause into a left-nested chain of binary [Logical] nodes.
func logicalFromAST(op string, arr []any) (Expr, error) {
	if len(arr) < 3 {
		return nil, fmt.Errorf("puppetdb: ast: %q takes at least two operands", op)
	}
	acc, err := exprFromAST(arr[1])
	if err != nil {
		return nil, err
	}
	for _, sub := range arr[2:] {
		right, err := exprFromAST(sub)
		if err != nil {
			return nil, err
		}
		acc = Logical{Op: op, Left: acc, Right: right}
	}
	return acc, nil
}

// notFromAST parses ["not", clause].
func notFromAST(arr []any) (Expr, error) {
	if len(arr) != 2 {
		return nil, fmt.Errorf("puppetdb: ast: not takes exactly one operand")
	}
	inner, err := exprFromAST(arr[1])
	if err != nil {
		return nil, err
	}
	return Not{Expr: inner}, nil
}

// isNullFromAST parses ["null?", field, bool].
func isNullFromAST(arr []any) (Expr, error) {
	if len(arr) != 3 {
		return nil, fmt.Errorf("puppetdb: ast: null? takes a field and a boolean")
	}
	field, ok := arr[1].(string)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: null? field must be a string")
	}
	isNull, ok := arr[2].(bool)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: null? flag must be a boolean")
	}
	return IsNull{Field: field, Negate: !isNull}, nil
}

// regexpArrayFromAST parses ["~>", field, [pattern, ...]].
func regexpArrayFromAST(arr []any) (Expr, error) {
	if len(arr) != 3 {
		return nil, fmt.Errorf("puppetdb: ast: ~> takes a field and a pattern list")
	}
	field, ok := arr[1].(string)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: ~> field must be a string")
	}
	pats, err := astStringList(arr[2])
	if err != nil {
		return nil, fmt.Errorf("puppetdb: ast: ~> patterns: %w", err)
	}
	return RegexpArray{Field: field, Patterns: pats}, nil
}

// subqueryFromAST parses ["subquery", entity] or ["subquery", entity, filter].
func subqueryFromAST(arr []any) (Expr, error) {
	if len(arr) < 2 || len(arr) > 3 {
		return nil, fmt.Errorf("puppetdb: ast: subquery takes an entity and an optional filter")
	}
	entity, ok := arr[1].(string)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: subquery entity must be a string")
	}
	if !entities[entity] {
		return nil, fmt.Errorf("puppetdb: ast: unknown subquery entity %q", entity)
	}
	sq := Subquery{Entity: entity}
	if len(arr) == 3 {
		filter, err := exprFromAST(arr[2])
		if err != nil {
			return nil, err
		}
		sq.Filter = filter
	}
	return sq, nil
}

// inFromAST parses ["in", lhs, ["array", [...]]] or ["in", lhs, ["from", ...]].
func inFromAST(arr []any) (Expr, error) {
	if len(arr) != 3 {
		return nil, fmt.Errorf("puppetdb: ast: in takes a left-hand side and a right-hand side")
	}
	fields, err := inFields(arr[1])
	if err != nil {
		return nil, err
	}
	head, rhs, ok := nodeHead(arr[2])
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: in right-hand side must be an array or subquery")
	}
	switch head {
	case "array":
		return inArray(fields, rhs)
	case "from":
		sub, err := queryFromAST(rhs)
		if err != nil {
			return nil, err
		}
		if len(sub.Projection) == 0 {
			return nil, fmt.Errorf("puppetdb: ast: in subquery requires an extract projection")
		}
		return In{Fields: fields, Sub: sub}, nil
	default:
		return nil, fmt.Errorf("puppetdb: ast: in right-hand side must be [\"array\", ...] or [\"from\", ...]")
	}
}

// inFields resolves the left-hand side of an in clause: a single field string or
// an array of field strings.
func inFields(v any) ([]string, error) {
	if s, ok := v.(string); ok {
		return []string{s}, nil
	}
	fields, err := astStringList(v)
	if err != nil {
		return nil, fmt.Errorf("puppetdb: ast: in left-hand side: %w", err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("puppetdb: ast: in left-hand side field list is empty")
	}
	return fields, nil
}

// inArray parses the ["array", [literal, ...]] right-hand side.
func inArray(fields []string, rhs []any) (Expr, error) {
	if len(rhs) != 2 {
		return nil, fmt.Errorf("puppetdb: ast: array takes exactly one value list")
	}
	vals, ok := rhs[1].([]any)
	if !ok {
		return nil, fmt.Errorf("puppetdb: ast: array values must be a list")
	}
	lits := make([]Literal, len(vals))
	for i, v := range vals {
		lit, err := literalFromAny(v)
		if err != nil {
			return nil, err
		}
		lits[i] = lit
	}
	return In{Fields: fields, Array: lits}, nil
}

// literalFromAny converts a decoded JSON scalar into a [Literal].
func literalFromAny(v any) (Literal, error) {
	switch x := v.(type) {
	case nil:
		return Literal{Kind: LitNull}, nil
	case bool:
		return Literal{Kind: LitBool, Bool: x}, nil
	case float64:
		return Literal{Kind: LitNumber, Num: x}, nil
	case string:
		return Literal{Kind: LitString, Str: x}, nil
	default:
		return Literal{}, fmt.Errorf("puppetdb: ast: unsupported literal %T", v)
	}
}

// astStringList converts a decoded value into a slice of strings.
func astStringList(v any) ([]string, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array")
	}
	out := make([]string, len(arr))
	for i, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("element %d is not a string", i)
		}
		out[i] = s
	}
	return out, nil
}

// astInt extracts a non-negative integer from arr[idx] for a numeric modifier.
func astInt(arr []any, idx int, what string) (int, error) {
	if len(arr) != idx+1 {
		return 0, fmt.Errorf("puppetdb: ast: %s takes exactly one value", what)
	}
	f, ok := arr[idx].(float64)
	if !ok || f != float64(int(f)) || f < 0 {
		return 0, fmt.Errorf("puppetdb: ast: %s must be a non-negative integer", what)
	}
	return int(f), nil
}
