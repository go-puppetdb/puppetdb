// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"fmt"
	"sort"
	"strings"
)

// aggregate evaluates the group-by and function projections of a query against
// the already-filtered rows. Without a group-by clause it produces exactly one
// output row (the aggregate over every matched row, so count() of no rows is
// still one row with count 0). With a group-by clause it produces one row per
// distinct group, in a deterministic key order.
func aggregate(q *Query, rows []Row) ([]Row, error) {
	if len(q.GroupBy) == 0 {
		row, err := computeRow(q, rows)
		if err != nil {
			return nil, err
		}
		return []Row{row}, nil
	}

	keys, groups, err := groupRows(q.GroupBy, rows)
	if err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(keys))
	for _, k := range keys {
		row, err := computeRow(q, groups[k])
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// groupRows buckets rows by their group-by key, returning the keys in sorted
// order (for deterministic output) and the per-key row groups.
func groupRows(groupBy []ProjItem, rows []Row) ([]string, map[string][]Row, error) {
	groups := map[string][]Row{}
	var keys []string
	for _, row := range rows {
		vals, err := groupKeyValues(groupBy, row)
		if err != nil {
			return nil, nil, err
		}
		k := keyString(vals)
		if _, seen := groups[k]; !seen {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], row)
	}
	sort.Strings(keys)
	return keys, groups, nil
}

// groupKeyValues evaluates each group-by item against a row. Function group-by
// items (for example to_string) are not evaluated in-memory and error.
func groupKeyValues(groupBy []ProjItem, row Row) ([]any, error) {
	vals := make([]any, len(groupBy))
	for i, it := range groupBy {
		if it.Func != nil {
			return nil, fmt.Errorf("puppetdb: eval: grouping by function %q is not evaluated in-memory", it.Func.Name)
		}
		vals[i], _ = digField(row, it.Field)
	}
	return vals, nil
}

// keyString renders a group key tuple into a stable string that distinguishes
// values by both type and content (so 1 and "1" never collide).
func keyString(vals []any) string {
	var b strings.Builder
	for i, v := range vals {
		if i > 0 {
			b.WriteByte(0)
		}
		fmt.Fprintf(&b, "%T=%v", v, v)
	}
	return b.String()
}

// computeRow builds one output row for a group. Plain projected fields take
// their value from the group's first row (they are constant within a group when
// grouped); functions are aggregated over the whole group. With no projection
// the group-by fields themselves form the row.
func computeRow(q *Query, rows []Row) (Row, error) {
	items := q.Projection
	if len(items) == 0 {
		items = q.GroupBy
	}
	r := Row{}
	for _, it := range items {
		if it.Func != nil {
			v, err := aggFunc(it.Func, rows)
			if err != nil {
				return nil, err
			}
			r[it.Func.Name] = v
			continue
		}
		var v any
		if len(rows) > 0 {
			v, _ = digField(rows[0], it.Field)
		}
		r[it.Field] = v
	}
	return r, nil
}

// aggFunc computes a single aggregate function over the group's rows.
func aggFunc(f *Func, rows []Row) (any, error) {
	switch f.Name {
	case "count":
		return aggCount(f, rows), nil
	case "sum":
		s, _, err := aggSum(f, rows)
		if err != nil {
			return nil, err
		}
		return s, nil
	case "avg":
		return aggAvg(f, rows)
	case "min":
		return aggMinMax(f, rows, true)
	case "max":
		return aggMinMax(f, rows, false)
	default: // to_string
		return nil, fmt.Errorf("puppetdb: eval: function %q is compiled to AST but not evaluated in-memory", f.Name)
	}
}

// aggCount counts rows: count() counts every row; count(field) counts rows whose
// field is present and non-null (SQL semantics).
func aggCount(f *Func, rows []Row) int {
	if len(f.Args) == 0 {
		return len(rows)
	}
	field := f.Args[0].Text
	n := 0
	for _, row := range rows {
		if v, found := digField(row, field); found && v != nil {
			n++
		}
	}
	return n
}

// aggField returns the field name of an aggregate that requires one, erroring
// when the function was written without an argument.
func aggField(f *Func) (string, error) {
	if len(f.Args) == 0 {
		return "", fmt.Errorf("puppetdb: eval: function %q requires a field argument", f.Name)
	}
	return f.Args[0].Text, nil
}

// aggSum returns the sum of the numeric values of the aggregate's field and how
// many numeric values contributed. A sum over no numeric values is nil.
func aggSum(f *Func, rows []Row) (any, int, error) {
	field, err := aggField(f)
	if err != nil {
		return nil, 0, err
	}
	var sum float64
	n := 0
	for _, row := range rows {
		v, found := digField(row, field)
		if !found {
			continue
		}
		if x, ok := toFloat(v); ok {
			sum += x
			n++
		}
	}
	if n == 0 {
		return nil, 0, nil
	}
	return sum, n, nil
}

// aggAvg returns the arithmetic mean of the numeric values of the field, or nil
// when there are none.
func aggAvg(f *Func, rows []Row) (any, error) {
	s, n, err := aggSum(f, rows)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	return s.(float64) / float64(n), nil
}

// aggMinMax returns the minimum (min true) or maximum (min false) of the
// comparable, non-null values of the field, or nil when there are none.
func aggMinMax(f *Func, rows []Row, min bool) (any, error) {
	field, err := aggField(f)
	if err != nil {
		return nil, err
	}
	var best any
	have := false
	for _, row := range rows {
		v, found := digField(row, field)
		if !found || v == nil {
			continue
		}
		if !have {
			best, have = v, true
			continue
		}
		if c, ok := orderedCompare(v, best); ok && ((min && c < 0) || (!min && c > 0)) {
			best = v
		}
	}
	if !have {
		return nil, nil
	}
	return best, nil
}
