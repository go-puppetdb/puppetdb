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

## Supported (v1)

| Area | Detail |
|------|--------|
| Entities | `nodes`, `resources`, `facts`, `inventory`, `catalogs`, `reports`, `events`, `edges`, `fact_contents` |
| Comparison | `=` `!=` `<` `>` `<=` `>=` |
| Regexp | `~` (match), `!~` (non-match) |
| Boolean | `and`, `or`, `not`, grouping with `(` `)` |
| Membership | `in` against an array literal `[...]` or a subquery `entity[proj]{...}` |
| Null tests | `is null`, `is not null` |
| Projection | extract field lists `entity[field, field]{...}`, dotted paths `facts.os.family` |
| Modifiers | `order by ... [asc\|desc]`, `limit`, `offset` |
| Literals | strings (with `\" \\ \n \t \r` escapes), integers, floats (incl. negative), `true`, `false`, `null` |
| Compilation | PQL AST → `["from", entity, ["and", ["=", "field", "x"], ...]]` JSON |
| Evaluation | in-memory store with recursive subquery evaluation, ordering, paging, projection |
| Client | POST PQL or compiled AST to `/pdb/query/v4`; token auth; injectable transport |

## Deferred (not in v1, documented — not silently capped)

- **A PuppetDB storage server.** This library queries; it does not persist,
  index or serve. There is no on-disk store and no command/ingest endpoint.
- **Importing a live PuppetDB's data** into the in-memory store (no sync/replay
  helpers yet — you populate `Store` yourself).
- **Aggregate / function projections**: `count()`, `avg()`, `group by`,
  `extract`-with-function and the `[...]` capture-group projections of PQL.
- **`~>` / dotted regexp array paths** on `fact_contents` `path`, and the
  `select_<entity>` legacy subquery spellings (the modern `in`/`from` forms are
  supported).
- **Structured-fact deep querying operators** beyond dotted-path equality.

## Development

```sh
go test -race -coverpkg=./... -coverprofile=cover.out ./...   # 100% coverage gate
go vet ./... && gofmt -l .
```

CGO is never required. CI builds and tests on `amd64`, `arm64`, `riscv64`,
`loong64`, `ppc64le` and `s390x`.

## License

BSD-3-Clause. Copyright (c) 2026, the go-puppetdb/puppetdb authors.
