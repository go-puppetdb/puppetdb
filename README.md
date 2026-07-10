# go-puppetdb/puppetdb

[![ci](https://github.com/go-puppetdb/puppetdb/actions/workflows/ci.yml/badge.svg)](https://github.com/go-puppetdb/puppetdb/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-puppetdb/puppetdb.svg)](https://pkg.go.dev/github.com/go-puppetdb/puppetdb)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A pragmatic, **pure-Go (CGO=0), standard-library-only** PuppetDB toolkit:

- a **PQL** (Puppet Query Language) lexer, parser and typed AST;
- a compiler from the AST to PuppetDB's **canonical AST-query JSON** wire form;
- an **in-memory evaluator** so PQL is useful and fully testable without a server;
- an **HTTP client** for a real PuppetDB `/pdb/query/v4` endpoint, behind an
  injectable `http.RoundTripper` seam.

```go
q, _ := puppetdb.Parse(`nodes[certname]{ certname in resources[certname]{ type = "Class" and title = "nginx" } } order by certname limit 10`)

// Compile to the canonical AST-query JSON the HTTP API accepts:
fmt.Println(string(q.MarshalAST()))

// Evaluate against an in-memory dataset:
store := puppetdb.NewStore()
store.Add("nodes", puppetdb.Row{"certname": "web1"})
store.Add("resources", puppetdb.Row{"certname": "web1", "type": "Class", "title": "nginx"})
rows, _ := store.Eval(q)

// Or query a live PuppetDB:
c := puppetdb.NewClient("https://puppetdb.example:8081", puppetdb.WithToken(token))
rows, _ = c.Query(context.Background(), `nodes{ facts.os.family = "RedHat" }`)
```

## Supported

| Area | Detail |
|------|--------|
| Entities | `nodes`, `resources`, `facts`, `inventory`, `catalogs`, `reports`, `events`, `edges`, `fact_contents`, `fact_paths`, `factsets`, `environments`, `packages`, `package_inventory` |
| Comparison | `=` `!=` `<` `>` `<=` `>=` |
| Regexp | `~` (match), `!~` (non-match), `~>` (regexp-array match) |
| Boolean | `and`, `or`, `not`, grouping with `(` `)` |
| Membership | `in` against an array literal `[...]` or a subquery `entity[proj]{...}` |
| Subqueries | explicit `in`/`from`, implicit `entity { ... }`, and the legacy `select_<entity>` spelling (all accepted; compiled to the canonical form) |
| Null tests | `is null`, `is not null` |
| Projection | extract lists `entity[field, count()]{...}`, dotted paths `facts.os.family`, aggregate/transform **functions** `count()`, `count(field)`, `avg`, `sum`, `min`, `max`, `to_string(field, fmt)` |
| Grouping | **`group by`** (fields or functions), accepted both inside the braces (PQL grammar) and, as a superset, after them |
| Modifiers | `order by ... [asc\|desc]`, `limit`, `offset` — inside or after the braces |
| Literals | strings — both `"double"` (with `\" \\ \n \t \r`) and `'single'` (with `\'`) — integers, floats incl. negative and **scientific notation** (`1.5e3`, `2E-4`), `true`, `false`, `null` |
| Fields | dotted deep paths and a trailing `?` (e.g. `deactivated?`) |
| Compilation | PQL → canonical AST-query JSON: `["from", entity, ["extract", [cols…], filter, ["group_by", …]], …]`, `["function", name, args…]`, `["~>", …]`, `["subquery", …]` |
| Evaluation | in-memory store: filter, recursive `in`-subquery evaluation, `group by` + `count/avg/sum/min/max` aggregation, `~>` array matching, ordering, paging, projection |
| Client | POST PQL or compiled AST to `/pdb/query/v4`; token auth; injectable transport |

The parser accepts every construct of PuppetDB's PQL grammar
(`pql-grammar.ebnf`) and compiles to the exact AST-query JSON PuppetDB's own
`transform.clj` produces (asserted by the differential tests).

## Not evaluated in-memory (compiled to AST for the server; client + eval only)

These parse and compile to the correct AST-query JSON (so the `Client` sends
them to a real PuppetDB), but the in-memory `Store` returns a clear error rather
than guessing:

- **`to_string(field, fmt)`** — requires PostgreSQL `to_char` date/number
  formatting.
- **Implicit subqueries** `entity { ... }` — require PuppetDB's entity join
  graph. Use the explicit `field in entity[field]{ ... }` form for in-memory
  evaluation.

## Out of scope (documented — not silently capped)

- **A PuppetDB storage server.** This library queries; it does not persist,
  index or serve. There is no on-disk store and no command/ingest endpoint.
- **Importing a live PuppetDB's data** into the in-memory store (no sync/replay
  helpers yet — you populate `Store` yourself).
- **Regexp capture-group projections** — not a PQL construct (PQL projects
  fields and functions only), so nothing to implement.

See [BENCHMARKS.md](BENCHMARKS.md) for the parse/compile/eval baselines.

## Development

```sh
go test -race -coverpkg=./... -coverprofile=cover.out ./...   # 100% coverage gate
go vet ./... && gofmt -l .
```

CGO is never required. CI builds and tests on `amd64`, `arm64`, `riscv64`,
`loong64`, `ppc64le` and `s390x`.

## License

BSD-3-Clause. Copyright (c) 2026, the go-puppetdb/puppetdb authors.
