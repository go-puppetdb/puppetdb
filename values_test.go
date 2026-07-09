// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import "testing"

func TestToFloat(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		ok   bool
	}{
		{float64(1.5), 1.5, true},
		{int(3), 3, true},
		{int64(7), 7, true},
		{"x", 0, false},
	}
	for _, c := range cases {
		got, ok := toFloat(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("toFloat(%v) = (%v,%v) want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestValueEqual(t *testing.T) {
	cases := []struct {
		a, b any
		want bool
	}{
		{nil, nil, true},
		{nil, 1, false},
		{1, nil, false},
		{int(2), float64(2), true},
		{float64(2), float64(3), false},
		{float64(2), "2", false},
		{"a", "a", true},
		{"a", "b", false},
		{"a", 1, false},
		{true, true, true},
		{true, false, false},
		{true, "true", false},
		{map[string]any{}, "x", false},
	}
	for _, c := range cases {
		if got := valueEqual(c.a, c.b); got != c.want {
			t.Fatalf("valueEqual(%v,%v) = %v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestOrderedCompare(t *testing.T) {
	cases := []struct {
		a, b any
		want int
		ok   bool
	}{
		{1, 2, -1, true},
		{2, 1, 1, true},
		{2, 2, 0, true},
		{1, "x", 0, false},
		{"a", "b", -1, true},
		{"b", "a", 1, true},
		{"a", "a", 0, true},
		{"a", 1, 0, false},
		{true, false, 0, false},
	}
	for _, c := range cases {
		got, ok := orderedCompare(c.a, c.b)
		if ok != c.ok || got != c.want {
			t.Fatalf("orderedCompare(%v,%v) = (%v,%v) want (%v,%v)", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestCompareAny(t *testing.T) {
	cases := []struct {
		a, b any
		want int
	}{
		{nil, nil, 0},
		{nil, 1, -1},
		{1, nil, 1},
		{1, 2, -1},
		{true, false, 0}, // non-comparable pair
	}
	for _, c := range cases {
		if got := compareAny(c.a, c.b); got != c.want {
			t.Fatalf("compareAny(%v,%v) = %v want %v", c.a, c.b, got, c.want)
		}
	}
}
