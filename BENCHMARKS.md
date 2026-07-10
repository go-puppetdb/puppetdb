# Benchmarks

This package is a pure-Go (CGO=0), standard-library-only PQL toolkit. There is
no single "reference" PQL parser to race against: PuppetDB's own PQL front end
is written in Clojure (Mark Engelberg's Instaparse over the `pql-grammar.ebnf`
grammar) and runs inside a JVM against a PostgreSQL backend, so a like-for-like
`ns/op` comparison is not meaningful. Instead we document a self-contained Go
baseline for the three stages of the pipeline and track it for regressions.

## What is measured

A representative **aggregate** query exercising the features added for full PQL
compatibility (filter + regexp + `group by` + aggregate function + paging):

```
facts[name, count(value)]{ certname ~ "^web" group by name } order by count desc limit 10
```

- `BenchmarkParseAggregate` — `Parse` (lex + recursive-descent parse) of the
  query string into a typed `*Query`.
- `BenchmarkCompileAggregate` — `Query.MarshalAST` (compile to canonical
  AST-query JSON + `encoding/json`).
- `BenchmarkEvalAggregate` — `Store.Eval` over a **10,000-row** in-memory facts
  dataset spread across five groups: filter, group, aggregate (`count`), sort
  and limit.

## Methodology

```sh
go test -run '^$' -bench=. -benchmem ./...
```

Run on an otherwise-idle machine; take the best of several runs. Compare across
commits with `benchstat`:

```sh
go test -run '^$' -bench=. -count=10 ./... > old.txt   # before a change
go test -run '^$' -bench=. -count=10 ./... > new.txt   # after
benchstat old.txt new.txt
```

## Baseline (Apple M4 Max, Go 1.26, `darwin/arm64`)

| Benchmark | time/op | B/op | allocs/op |
|-----------|--------:|-----:|----------:|
| `ParseAggregate`   | ~0.68 µs | 2536 | 15 |
| `CompileAggregate` | ~1.27 µs | 1200 | 35 |
| `EvalAggregate` (10k rows) | ~11 ms | 23 MB | 370k |

Parse and compile are sub-microsecond-to-microsecond and allocation-light. Eval
cost is dominated by the linear scan over the dataset and the per-group
accumulation; it scales linearly with row count. The in-memory evaluator is a
correctness/testing aid (the `Client` streams the same query to a real PuppetDB
for production-scale data), so its baseline is tracked for regressions rather
than raced against an external engine.

## How the comparison to PuppetDB's parser would work

If a differential throughput comparison is ever wanted, the fair approach is to
feed the *same* query strings to both front ends and compare **AST-JSON output
for equality** (which this package's tests already assert against the canonical
forms PuppetDB emits) rather than wall-clock: the Go parser produces the exact
`["from", …]` / `["extract", …, ["group_by", …]]` structures that PuppetDB's
`transform.clj` produces, so the two are behaviourally interchangeable on the
wire.
