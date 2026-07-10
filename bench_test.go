// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"fmt"
	"testing"
)

// aggQuery is a representative aggregate PQL query: a filtered, grouped count.
const aggQuery = `facts[name, count(value)]{ certname ~ "^web" group by name } order by count desc limit 10`

// benchStore builds a facts dataset of n rows spread across a handful of groups.
func benchStore(n int) *Store {
	s := NewStore()
	names := []string{"os", "kernel", "memory", "processors", "networking"}
	rows := make([]Row, n)
	for i := 0; i < n; i++ {
		rows[i] = Row{
			"certname": fmt.Sprintf("web%d.example.com", i%256),
			"name":     names[i%len(names)],
			"value":    float64(i % 1000),
		}
	}
	s.Add("facts", rows...)
	return s
}

// BenchmarkParseAggregate measures parsing (lex + parse) of the aggregate query.
func BenchmarkParseAggregate(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(aggQuery); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileAggregate measures AST-JSON compilation of the parsed query.
func BenchmarkCompileAggregate(b *testing.B) {
	q, err := Parse(aggQuery)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.MarshalAST()
	}
}

// BenchmarkEvalAggregate measures in-memory evaluation (filter + group + count +
// order + limit) of the aggregate query over a 10k-row dataset.
func BenchmarkEvalAggregate(b *testing.B) {
	q, err := Parse(aggQuery)
	if err != nil {
		b.Fatal(err)
	}
	s := benchStore(10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Eval(q); err != nil {
			b.Fatal(err)
		}
	}
}
