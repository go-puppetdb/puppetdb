// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// queryPath is the PuppetDB v4 query endpoint appended to the base URL.
const queryPath = "/pdb/query/v4"

// Client talks to a PuppetDB /pdb/query/v4 endpoint over HTTP. Its transport is
// an injectable [net/http.RoundTripper] seam, so callers can supply a
// TLS-configured transport, a token-authenticated one, or a fake for testing.
type Client struct {
	baseURL string
	token   string
	rt      http.RoundTripper
}

// Option configures a [Client].
type Option func(*Client)

// WithToken sets the X-Authentication token sent with every request.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithRoundTripper overrides the HTTP transport used by the client.
func WithRoundTripper(rt http.RoundTripper) Option {
	return func(c *Client) { c.rt = rt }
}

// NewClient returns a client for the PuppetDB instance at baseURL (for example
// "https://puppetdb.example:8081"). By default it uses
// [net/http.DefaultTransport].
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		rt:      http.DefaultTransport,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Query posts a PQL string to the endpoint and decodes the JSON result rows.
func (c *Client) Query(ctx context.Context, pql string) ([]Row, error) {
	body, _ := json.Marshal(map[string]any{"query": pql})
	return c.do(ctx, body)
}

// QueryAST compiles a parsed [Query] to canonical AST JSON, posts it to the
// endpoint and decodes the JSON result rows.
func (c *Client) QueryAST(ctx context.Context, q *Query) ([]Row, error) {
	body, _ := json.Marshal(map[string]any{"query": q.AST()})
	return c.do(ctx, body)
}

// do performs one POST to the query endpoint with the given JSON body.
func (c *Client) do(ctx context.Context, body []byte) ([]Row, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+queryPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("puppetdb: client: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("X-Authentication", c.token)
	}

	resp, err := c.rt.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("puppetdb: client: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("puppetdb: client: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var rows []Row
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("puppetdb: client: decode response: %w", err)
	}
	return rows, nil
}
