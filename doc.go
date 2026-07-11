// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

// Package puppetdb is a pragmatic, pure-Go (no cgo) PuppetDB toolkit.
//
// It provides three cooperating pieces, all implemented with the Go standard
// library only:
//
//   - A lexer, parser and typed AST for the Puppet Query Language (PQL): the
//     entity-oriented query surface exposed by PuppetDB's HTTP API. The parser
//     accepts every construct of PuppetDB's PQL grammar — projection (extract)
//     lists including aggregate/transform functions (count, avg, sum, min, max,
//     to_string), the group-by clause, the comparison operators (= != < > <=
//     >=), the scalar and array regexp operators (~ !~ ~>), boolean composition
//     (and / or / not), membership (in) against array literals and subqueries,
//     implicit and legacy select_ subqueries, is-null tests, dotted deep field
//     paths, single- and double-quoted strings, scientific-notation numbers,
//     and the order-by / limit / offset paging modifiers (inside or after the
//     braces).
//
//   - A compiler from the parsed AST to PuppetDB's canonical AST-query JSON —
//     the ["from", entity, ["and", ["=", "field", "value"], ...]] wire form
//     that the /pdb/query/v4 endpoint accepts directly. See [Query.AST] and
//     [Query.MarshalAST].
//
//   - An in-memory evaluator ([Store]) that runs a parsed PQL query against a
//     registered dataset of rows, so the query language is useful and fully
//     testable without a running server. It applies the filter, group-by and
//     aggregate functions (count/avg/sum/min/max), the ~> array match, and
//     ordering/paging/projection; in-operator subqueries are evaluated
//     recursively against the same store.
//
//   - An HTTP [Client] for a real PuppetDB /pdb/query/v4 endpoint. The client
//     posts either a PQL string or a compiled AST query and decodes the JSON
//     rows. Its transport is an injectable [net/http.RoundTripper] seam so
//     callers (and tests) can supply a fake, a TLS-configured transport or a
//     token-authenticated one without touching the network.
//
//   - An AST-query parser, [ParseAST], the inverse of [Query.AST]: it reads
//     PuppetDB's canonical ["from", ...] JSON back into a typed [Query], so the
//     package can serve the AST query form as well as compile to it. For every
//     query this package emits, re-compiling the parsed result reproduces the
//     original AST byte-for-byte.
//
//   - Command ingestion, [Store.Ingest], for the current PuppetDB command wire
//     formats — replace facts (v5), replace catalog (v9) and store report (v8).
//     Each command is expanded into the query entities PuppetDB derives from it
//     (facts, fact_contents, inventory, resources, edges, catalogs, reports,
//     events and nodes), so ingested data is immediately queryable.
//
//   - A pure-Go embedded storage backend: [Open], [Store.Save], [Store.Snapshot]
//     and [Load] persist a store to a JSON file with no external database.
//
//   - An HTTP [Server] that serves the query API at /pdb/query/v4 (the root
//     PQL/AST endpoint and the per-entity paths) and ingests commands at
//     /pdb/cmd/v1, backed by a [Store] and safe to serve concurrently.
//
// A handful of constructs parse and compile to the correct AST-query JSON (so
// the [Client] sends them to a real PuppetDB) but are not evaluated in the
// in-memory [Store], which reports a clear error instead: the to_string
// transform (needs PostgreSQL to_char formatting) and implicit subqueries
// (need PuppetDB's entity join graph). Named residuals (documented, not
// silently capped): older command wire-format versions, the latest_report_*
// node rollups, PuppetDB-identical catalog/report content hashes (the store
// uses deterministic SHA-1 content hashes instead), and the query-endpoint
// pagination/count HTTP headers. See the package README for the full matrix.
package puppetdb
