// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-puppetdb/puppetdb authors

package puppetdb

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// The three PuppetDB command names understood by [Store.Ingest], together with
// the wire-format version each expander implements.
const (
	// CmdReplaceFacts is the "replace facts" command name.
	CmdReplaceFacts = "replace facts"
	// CmdReplaceCatalog is the "replace catalog" command name.
	CmdReplaceCatalog = "replace catalog"
	// CmdStoreReport is the "store report" command name.
	CmdStoreReport = "store report"

	factsVersion   = 5 // facts wire format v5
	catalogVersion = 9 // catalog wire format v9
	reportVersion  = 8 // report wire format v8
)

// Ingest applies a PuppetDB command payload to the store, expanding it into the
// query entities PuppetDB would derive from it. The recognised commands are
// [CmdReplaceFacts] (v5), [CmdReplaceCatalog] (v9) and [CmdStoreReport] (v8) —
// the current wire formats. "replace" commands replace the affected entities'
// rows for the command's certname; "store report" appends.
func (s *Store) Ingest(command string, version int, payload []byte) error {
	switch command {
	case CmdReplaceFacts:
		return s.ingestFacts(version, payload)
	case CmdReplaceCatalog:
		return s.ingestCatalog(version, payload)
	case CmdStoreReport:
		return s.ingestReport(version, payload)
	default:
		return fmt.Errorf("puppetdb: ingest: unknown command %q", command)
	}
}

// factsPayload is the "replace facts" v5 wire format.
type factsPayload struct {
	Certname          string         `json:"certname"`
	Environment       string         `json:"environment"`
	ProducerTimestamp string         `json:"producer_timestamp"`
	Producer          *string        `json:"producer"`
	Values            map[string]any `json:"values"`
	PackageInventory  [][]any        `json:"package_inventory"`
}

// ingestFacts expands a replace-facts command into the facts, fact_contents,
// inventory and nodes entities.
func (s *Store) ingestFacts(version int, payload []byte) error {
	if version != factsVersion {
		return fmt.Errorf("puppetdb: ingest: unsupported replace facts version %d (want %d)", version, factsVersion)
	}
	var f factsPayload
	if err := json.Unmarshal(payload, &f); err != nil {
		return fmt.Errorf("puppetdb: ingest: replace facts: %w", err)
	}
	if f.Certname == "" {
		return fmt.Errorf("puppetdb: ingest: replace facts: missing certname")
	}

	s.replaceFor("facts", f.Certname)
	s.replaceFor("fact_contents", f.Certname)
	s.replaceFor("inventory", f.Certname)

	names := sortedKeys(f.Values)
	for _, name := range names {
		val := f.Values[name]
		s.Add("facts", Row{
			"certname":    f.Certname,
			"environment": f.Environment,
			"name":        name,
			"value":       val,
		})
		for _, fc := range factContents(name, val) {
			s.Add("fact_contents", Row{
				"certname":    f.Certname,
				"environment": f.Environment,
				"path":        fc.path,
				"name":        fc.name,
				"value":       fc.value,
			})
		}
	}

	var trusted any
	if tv, ok := f.Values["trusted"].(map[string]any); ok {
		trusted = tv
	}
	s.Add("inventory", Row{
		"certname":    f.Certname,
		"environment": f.Environment,
		"timestamp":   f.ProducerTimestamp,
		"facts":       f.Values,
		"trusted":     trusted,
	})

	s.upsertNode(f.Certname, Row{
		"facts_environment": f.Environment,
		"facts_timestamp":   f.ProducerTimestamp,
	})
	return nil
}

// leaf is one flattened fact_contents entry.
type leaf struct {
	path  []any
	name  string
	value any
}

// factContents flattens a structured fact value into its leaf paths, mirroring
// PuppetDB's fact_contents entity. Scalars yield a single entry; maps and arrays
// recurse, appending the string key or integer index to the path.
func factContents(name string, value any) []leaf {
	return flattenFact([]any{name}, value)
}

// flattenFact recursively walks a fact value, accumulating the path.
func flattenFact(path []any, value any) []leaf {
	switch v := value.(type) {
	case map[string]any:
		var out []leaf
		for _, k := range sortedKeys(v) {
			child := append(append([]any{}, path...), k)
			out = append(out, flattenFact(child, v[k])...)
		}
		return out
	case []any:
		var out []leaf
		for i, e := range v {
			child := append(append([]any{}, path...), i)
			out = append(out, flattenFact(child, e)...)
		}
		return out
	default:
		return []leaf{{path: path, name: lastPathName(path), value: v}}
	}
}

// lastPathName renders the final path element as the fact_contents name.
func lastPathName(path []any) string {
	last := path[len(path)-1]
	if s, ok := last.(string); ok {
		return s
	}
	return strconv.Itoa(last.(int))
}

// catalogPayload is the "replace catalog" v9 wire format.
type catalogPayload struct {
	Certname          string            `json:"certname"`
	Version           string            `json:"version"`
	Environment       string            `json:"environment"`
	TransactionUUID   string            `json:"transaction_uuid"`
	CatalogUUID       string            `json:"catalog_uuid"`
	CodeID            string            `json:"code_id"`
	ProducerTimestamp string            `json:"producer_timestamp"`
	Producer          *string           `json:"producer"`
	Resources         []catalogResource `json:"resources"`
	Edges             []catalogEdge     `json:"edges"`
}

// catalogResource is one entry of a catalog's resources array.
type catalogResource struct {
	Type       string         `json:"type"`
	Title      string         `json:"title"`
	Aliases    []string       `json:"aliases"`
	Exported   bool           `json:"exported"`
	File       *string        `json:"file"`
	Line       *int           `json:"line"`
	Tags       []string       `json:"tags"`
	Parameters map[string]any `json:"parameters"`
}

// resourceSpec is the {type, title} pair used by catalog edges.
type resourceSpec struct {
	Type  string `json:"type"`
	Title string `json:"title"`
}

// catalogEdge is one entry of a catalog's edges array.
type catalogEdge struct {
	Source       resourceSpec `json:"source"`
	Target       resourceSpec `json:"target"`
	Relationship string       `json:"relationship"`
}

// ingestCatalog expands a replace-catalog command into the catalogs, resources,
// edges and nodes entities.
func (s *Store) ingestCatalog(version int, payload []byte) error {
	if version != catalogVersion {
		return fmt.Errorf("puppetdb: ingest: unsupported replace catalog version %d (want %d)", version, catalogVersion)
	}
	var c catalogPayload
	if err := json.Unmarshal(payload, &c); err != nil {
		return fmt.Errorf("puppetdb: ingest: replace catalog: %w", err)
	}
	if c.Certname == "" {
		return fmt.Errorf("puppetdb: ingest: replace catalog: missing certname")
	}

	s.replaceFor("catalogs", c.Certname)
	s.replaceFor("resources", c.Certname)
	s.replaceFor("edges", c.Certname)

	for _, r := range c.Resources {
		s.Add("resources", Row{
			"certname":    c.Certname,
			"environment": c.Environment,
			"resource":    resourceHash(c.Certname, r),
			"type":        r.Type,
			"title":       r.Title,
			"exported":    r.Exported,
			"tags":        stringSlice(r.Tags),
			"file":        strPtr(r.File),
			"line":        intPtr(r.Line),
			"parameters":  r.Parameters,
		})
	}
	for _, e := range c.Edges {
		s.Add("edges", Row{
			"certname":     c.Certname,
			"relationship": e.Relationship,
			"source_type":  e.Source.Type,
			"source_title": e.Source.Title,
			"target_type":  e.Target.Type,
			"target_title": e.Target.Title,
		})
	}
	s.Add("catalogs", Row{
		"certname":           c.Certname,
		"version":            c.Version,
		"environment":        c.Environment,
		"transaction_uuid":   c.TransactionUUID,
		"catalog_uuid":       c.CatalogUUID,
		"code_id":            c.CodeID,
		"producer_timestamp": c.ProducerTimestamp,
		"producer":           strPtr(c.Producer),
		"hash":               contentHash(payload),
	})

	s.upsertNode(c.Certname, Row{
		"catalog_environment": c.Environment,
		"catalog_timestamp":   c.ProducerTimestamp,
	})
	return nil
}

// reportPayload is the "store report" v8 wire format.
type reportPayload struct {
	Certname             string           `json:"certname"`
	Environment          string           `json:"environment"`
	PuppetVersion        string           `json:"puppet_version"`
	ReportFormat         any              `json:"report_format"`
	ConfigurationVersion any              `json:"configuration_version"`
	StartTime            string           `json:"start_time"`
	EndTime              string           `json:"end_time"`
	ProducerTimestamp    string           `json:"producer_timestamp"`
	Producer             *string          `json:"producer"`
	TransactionUUID      string           `json:"transaction_uuid"`
	CatalogUUID          string           `json:"catalog_uuid"`
	CodeID               string           `json:"code_id"`
	CachedCatalogStatus  string           `json:"cached_catalog_status"`
	Status               string           `json:"status"`
	Noop                 bool             `json:"noop"`
	NoopPending          bool             `json:"noop_pending"`
	CorrectiveChange     *bool            `json:"corrective_change"`
	Resources            []reportResource `json:"resources"`
}

// reportResource is one entry of a report's resources array.
type reportResource struct {
	Timestamp       string        `json:"timestamp"`
	ResourceType    string        `json:"resource_type"`
	ResourceTitle   string        `json:"resource_title"`
	Skipped         bool          `json:"skipped"`
	File            *string       `json:"file"`
	Line            *int          `json:"line"`
	ContainmentPath []string      `json:"containment_path"`
	Events          []reportEvent `json:"events"`
}

// reportEvent is one entry of a report resource's events array.
type reportEvent struct {
	Status           *string `json:"status"`
	Timestamp        string  `json:"timestamp"`
	Property         *string `json:"property"`
	Name             *string `json:"name"`
	NewValue         any     `json:"new_value"`
	OldValue         any     `json:"old_value"`
	Message          *string `json:"message"`
	CorrectiveChange *bool   `json:"corrective_change"`
}

// ingestReport expands a store-report command into the reports, events and nodes
// entities. Reports accumulate: existing rows for the certname are kept.
func (s *Store) ingestReport(version int, payload []byte) error {
	if version != reportVersion {
		return fmt.Errorf("puppetdb: ingest: unsupported store report version %d (want %d)", version, reportVersion)
	}
	var r reportPayload
	if err := json.Unmarshal(payload, &r); err != nil {
		return fmt.Errorf("puppetdb: ingest: store report: %w", err)
	}
	if r.Certname == "" {
		return fmt.Errorf("puppetdb: ingest: store report: missing certname")
	}
	hash := contentHash(payload)

	s.Add("reports", Row{
		"hash":                  hash,
		"certname":              r.Certname,
		"environment":           r.Environment,
		"status":                r.Status,
		"noop":                  r.Noop,
		"noop_pending":          r.NoopPending,
		"puppet_version":        r.PuppetVersion,
		"report_format":         r.ReportFormat,
		"configuration_version": r.ConfigurationVersion,
		"start_time":            r.StartTime,
		"end_time":              r.EndTime,
		"producer_timestamp":    r.ProducerTimestamp,
		"producer":              strPtr(r.Producer),
		"transaction_uuid":      r.TransactionUUID,
		"catalog_uuid":          r.CatalogUUID,
		"code_id":               r.CodeID,
		"cached_catalog_status": r.CachedCatalogStatus,
		"corrective_change":     boolPtr(r.CorrectiveChange),
	})

	for _, res := range r.Resources {
		for _, ev := range res.Events {
			s.Add("events", Row{
				"certname":          r.Certname,
				"report":            hash,
				"environment":       r.Environment,
				"run_start_time":    r.StartTime,
				"run_end_time":      r.EndTime,
				"status":            strPtr(ev.Status),
				"timestamp":         ev.Timestamp,
				"resource_type":     res.ResourceType,
				"resource_title":    res.ResourceTitle,
				"property":          strPtr(ev.Property),
				"name":              strPtr(ev.Name),
				"new_value":         ev.NewValue,
				"old_value":         ev.OldValue,
				"message":           strPtr(ev.Message),
				"file":              strPtr(res.File),
				"line":              intPtr(res.Line),
				"containment_path":  stringSlice(res.ContainmentPath),
				"corrective_change": boolPtr(ev.CorrectiveChange),
			})
		}
	}

	s.upsertNode(r.Certname, Row{
		"report_environment":   r.Environment,
		"report_timestamp":     r.ProducerTimestamp,
		"latest_report_hash":   hash,
		"latest_report_status": r.Status,
		"latest_report_noop":   r.Noop,
	})
	return nil
}

// replaceFor removes every row of the named entity whose certname matches.
func (s *Store) replaceFor(entity, certname string) {
	rows := s.entities[entity]
	kept := rows[:0]
	for _, row := range rows {
		if row["certname"] != certname {
			kept = append(kept, row)
		}
	}
	if len(kept) == 0 {
		delete(s.entities, entity)
		return
	}
	s.entities[entity] = kept
}

// upsertNode merges fields into the node row for certname, creating it if absent.
func (s *Store) upsertNode(certname string, fields Row) {
	for _, row := range s.entities["nodes"] {
		if row["certname"] == certname {
			for k, v := range fields {
				row[k] = v
			}
			return
		}
	}
	row := Row{"certname": certname}
	for k, v := range fields {
		row[k] = v
	}
	s.Add("nodes", row)
}

// sortedKeys returns the keys of m in deterministic order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// contentHash is a deterministic SHA-1 of the raw command payload. It is stable
// and content-addressed but is NOT PuppetDB's own catalog/report hash, which
// canonicalises fields differently.
func contentHash(payload []byte) string {
	sum := sha1.Sum(payload)
	return hex.EncodeToString(sum[:])
}

// resourceHash is a deterministic SHA-1 identifying a catalog resource.
func resourceHash(certname string, r catalogResource) string {
	b, _ := json.Marshal([]any{certname, r.Type, r.Title, r.Parameters})
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}

// strPtr renders a *string as its value or nil.
func strPtr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// intPtr renders a *int as a float64 (JSON number) or nil.
func intPtr(p *int) any {
	if p == nil {
		return nil
	}
	return float64(*p)
}

// boolPtr renders a *bool as its value or nil.
func boolPtr(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}

// stringSlice converts a []string to a []any, preserving nil.
func stringSlice(ss []string) any {
	if ss == nil {
		return nil
	}
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
