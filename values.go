// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

// toFloat converts a numeric row value to float64. JSON decoding yields float64;
// int and int64 are also accepted for hand-built rows.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// valueEqual reports whether two values are equal under PQL semantics: numbers
// compare numerically (across int/float), strings and booleans by identity, and
// nil equals only nil.
func valueEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			return af == bf
		}
		return false
	}
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return false
	}
}

// orderedCompare returns -1, 0 or 1 comparing a and b, plus whether the pair is
// comparable (both numeric, or both strings).
func orderedCompare(a, b any) (int, bool) {
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			switch {
			case af < bf:
				return -1, true
			case af > bf:
				return 1, true
			default:
				return 0, true
			}
		}
		return 0, false
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		switch {
		case as < bs:
			return -1, true
		case as > bs:
			return 1, true
		default:
			return 0, true
		}
	}
	return 0, false
}

// compareAny orders two values for order-by: nil sorts first, then numbers and
// strings compare naturally; otherwise the pair is treated as equal (stable).
func compareAny(a, b any) int {
	an, bn := a == nil, b == nil
	switch {
	case an && bn:
		return 0
	case an:
		return -1
	case bn:
		return 1
	}
	if c, ok := orderedCompare(a, b); ok {
		return c
	}
	return 0
}
