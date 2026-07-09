// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// fakeRT is a RoundTripper backed by a function, for testing without a network.
type fakeRT struct {
	fn func(*http.Request) (*http.Response, error)
}

func (f fakeRT) RoundTrip(r *http.Request) (*http.Response, error) { return f.fn(r) }

// resp builds a *http.Response with the given status and body.
func resp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestClientQuerySuccess(t *testing.T) {
	var gotBody, gotAuth, gotContentType, gotURL, gotMethod string
	rt := fakeRT{fn: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotAuth = r.Header.Get("X-Authentication")
		gotContentType = r.Header.Get("Content-Type")
		gotURL = r.URL.String()
		gotMethod = r.Method
		return resp(http.StatusOK, `[{"certname":"web1"},{"certname":"web2"}]`), nil
	}}
	c := NewClient("https://pdb.example:8081/", WithToken("secret"), WithRoundTripper(rt))

	rows, err := c.Query(context.Background(), `nodes{}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{{"certname": "web1"}, {"certname": "web2"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows: got %v want %v", rows, want)
	}
	if gotBody != `{"query":"nodes{}"}`+"\n" && gotBody != `{"query":"nodes{}"}` {
		t.Fatalf("body: got %q", gotBody)
	}
	if gotAuth != "secret" {
		t.Fatalf("auth header: got %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type: got %q", gotContentType)
	}
	if gotURL != "https://pdb.example:8081/pdb/query/v4" {
		t.Fatalf("url: got %q", gotURL)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method: got %q", gotMethod)
	}
}

func TestClientQueryASTSuccessNoToken(t *testing.T) {
	var gotBody, gotAuth string
	rt := fakeRT{fn: func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotAuth = r.Header.Get("X-Authentication")
		return resp(http.StatusOK, `[]`), nil
	}}
	c := NewClient("https://pdb.example:8081", WithRoundTripper(rt))

	q, err := Parse(`nodes[certname]{ certname = "web1" }`)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := c.QueryAST(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %v", rows)
	}
	if gotAuth != "" {
		t.Fatalf("expected no auth header, got %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"query":["from","nodes"`) {
		t.Fatalf("AST body: got %q", gotBody)
	}
}

func TestClientBuildRequestError(t *testing.T) {
	c := NewClient("http://\x7f", WithRoundTripper(fakeRT{fn: func(*http.Request) (*http.Response, error) {
		t.Fatal("transport should not be reached")
		return nil, nil
	}}))
	if _, err := c.Query(context.Background(), `nodes{}`); err == nil {
		t.Fatal("expected build-request error")
	}
}

func TestClientTransportError(t *testing.T) {
	rt := fakeRT{fn: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	}}
	c := NewClient("https://pdb.example", WithRoundTripper(rt))
	if _, err := c.Query(context.Background(), `nodes{}`); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestClientStatusError(t *testing.T) {
	rt := fakeRT{fn: func(*http.Request) (*http.Response, error) {
		return resp(http.StatusInternalServerError, "boom"), nil
	}}
	c := NewClient("https://pdb.example", WithRoundTripper(rt))
	_, err := c.Query(context.Background(), `nodes{}`)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 status error, got %v", err)
	}
}

func TestClientDecodeError(t *testing.T) {
	rt := fakeRT{fn: func(*http.Request) (*http.Response, error) {
		return resp(http.StatusOK, `not json`), nil
	}}
	c := NewClient("https://pdb.example", WithRoundTripper(rt))
	if _, err := c.Query(context.Background(), `nodes{}`); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestNewClientDefaultTransport(t *testing.T) {
	c := NewClient("https://pdb.example")
	if c.rt != http.DefaultTransport {
		t.Fatal("expected default transport")
	}
}
