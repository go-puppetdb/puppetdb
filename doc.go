// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

// Package puppetdb is a pragmatic, pure-Go (no cgo) PuppetDB toolkit.
//
// It provides three cooperating pieces, all implemented with the Go standard
// library only:
//
//   - A lexer, parser and typed AST for the Puppet Query Language (PQL): the
//     entity-oriented query surface exposed by PuppetDB's HTTP API. Supported
//     entities are nodes, resources, facts, inventory, catalogs, reports,
//     events, edges and fact_contents. Supported grammar covers projection
//     (extract) field lists, the comparison operators (= != < > <= >=), the
//     regexp match operators (~ !~), boolean composition (and / or / not),
//     membership (in) against both array literals and subqueries, is-null
//     tests, and the order-by / limit / offset paging modifiers.
//
//   - A compiler from the parsed AST to PuppetDB's canonical AST-query JSON —
//     the ["from", entity, ["and", ["=", "field", "value"], ...]] wire form
//     that the /pdb/query/v4 endpoint accepts directly. See [Query.AST] and
//     [Query.MarshalAST].
//
//   - An in-memory evaluator ([Store]) that runs a parsed PQL query against a
//     registered dataset of rows, so the query language is useful and fully
//     testable without a running server. Subqueries used by the in operator
//     are evaluated recursively against the same store.
//
//   - An HTTP [Client] for a real PuppetDB /pdb/query/v4 endpoint. The client
//     posts either a PQL string or a compiled AST query and decodes the JSON
//     rows. Its transport is an injectable [net/http.RoundTripper] seam so
//     callers (and tests) can supply a fake, a TLS-configured transport or a
//     token-authenticated one without touching the network.
//
// Out of scope for v1 (documented, not silently capped): a PuppetDB storage
// server, command/ingest endpoints, importing a live PuppetDB's data, and the
// aggregate/function projections (count(), avg(), group by) of PQL. See the
// package README for the full supported/deferred matrix.
package puppetdb
