package jsonstrict_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/13rac1/jsonstrict"
)

// fuzzNested gives the fuzz target a nested schema to walk.
type fuzzNested struct {
	Label string `json:"label"`
}

// fuzzTarget exercises flat fields, a nested struct, and a slice of structs.
type fuzzTarget struct {
	Name   string       `json:"name"`
	Value  int          `json:"value"`
	OK     bool         `json:"ok"`
	Nested fuzzNested   `json:"nested"`
	Items  []fuzzNested `json:"items,omitempty"`
}

// fuzzKnownTopLevel are the JSON keys fuzzTarget declares, in bracket-path
// form; they must never be reported unknown at the top level.
var fuzzKnownTopLevel = []string{`["name"]`, `["value"]`, `["ok"]`, `["nested"]`, `["items"]`}

// validMissingPath reports whether p is a path fuzzTarget could legitimately
// report missing: a required top-level key, the nested struct's field, or a
// slice element's field. Items itself is omitempty and never missing.
func validMissingPath(p string) bool {
	switch p {
	case `["name"]`, `["value"]`, `["ok"]`, `["nested"]`, `["nested"]["label"]`:
		return true
	}
	return strings.HasPrefix(p, `["items"][`) && strings.HasSuffix(p, `]["label"]`)
}

// FuzzUnmarshal differentially tests Unmarshal against plain json.Unmarshal
// and checks the diagnostic invariants on arbitrary input.
func FuzzUnmarshal(f *testing.F) {
	f.Add([]byte(`{"name":"test","value":42,"ok":true}`))
	f.Add([]byte(`{"name":"test","value":42,"ok":true,"extra":"field"}`))
	f.Add([]byte(`{"unknown_field":123}`))
	f.Add([]byte(`{"nested":{"label":"x","bogus":1}}`))
	f.Add([]byte(`{"items":[{"label":"a"},{"wrong":2}]}`))
	f.Add([]byte(`{"nested":null,"items":null}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"name":null,"value":"not_int"}`))
	f.Add([]byte(`{"name":"a","name":"b"}`))   // duplicate known key
	f.Add([]byte(`{"dup":1,"dup":2,"dup":3}`)) // duplicate unknown key
	f.Add([]byte("{\"name\":\"\xff\"}"))       // invalid UTF-8 value
	f.Add([]byte("{\"\xff\":1}"))              // invalid UTF-8 key
	f.Add([]byte(`[{"a":1,"a":2}]`))           // top-level array

	f.Fuzz(func(t *testing.T, data []byte) {
		var got fuzzTarget
		result, err := jsonstrict.Unmarshal(data, &got)

		// The decode and its error must match plain encoding/json exactly.
		var want fuzzTarget
		wantErr := json.Unmarshal(data, &want)
		if (err == nil) != (wantErr == nil) {
			t.Fatalf("error mismatch: jsonstrict=%v stdlib=%v", err, wantErr)
		}
		if err != nil && err.Error() != wantErr.Error() {
			t.Errorf("error text mismatch: jsonstrict=%q stdlib=%q", err, wantErr)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("decode mismatch: jsonstrict=%+v stdlib=%+v", got, want)
		}

		// Unknown raw values must be valid JSON, and declared keys must
		// never be reported unknown at the top level.
		for path, raw := range result.Unknown {
			if !json.Valid(raw) {
				t.Errorf("unknown %q has invalid raw value %q", path, raw)
			}
			if slices.Contains(fuzzKnownTopLevel, path) {
				t.Errorf("declared key %q reported unknown", path)
			}
		}

		// Missing must be sorted, unique, plausible for the schema, and
		// disjoint from Unknown.
		if !slices.IsSorted(result.Missing) {
			t.Errorf("Missing not sorted: %v", result.Missing)
		}
		for i, p := range result.Missing {
			if i > 0 && p == result.Missing[i-1] {
				t.Errorf("duplicate missing path %q", p)
			}
			if !validMissingPath(p) {
				t.Errorf("impossible missing path %q", p)
			}
			if _, ok := result.Unknown[p]; ok {
				t.Errorf("path %q both unknown and missing", p)
			}
		}

		// Structural diagnostics must be sorted and unique. Paths are non-empty
		// except the root (""), which only a top-level scalar can produce.
		for _, list := range [][]string{result.Duplicates, result.InvalidUTF8} {
			if !slices.IsSorted(list) {
				t.Errorf("structural list not sorted: %v", list)
			}
			for i, p := range list {
				if i > 0 && p == list[i-1] {
					t.Errorf("duplicate structural path %q in %v", p, list)
				}
			}
		}
		// Duplicate keys always live in an object, so their paths are never root.
		for _, p := range result.Duplicates {
			if p == "" {
				t.Errorf("duplicate path is empty (root): %v", result.Duplicates)
			}
		}

		// Err must agree with the diagnostics.
		hasDiag := len(result.Unknown) > 0 || len(result.Missing) > 0 ||
			len(result.Duplicates) > 0 || len(result.InvalidUTF8) > 0
		if hasDiag != (result.Err() != nil) {
			t.Errorf("Err()=%v inconsistent with unknown=%v missing=%v duplicates=%v invalidUTF8=%v",
				result.Err(), result.Unknown, result.Missing, result.Duplicates, result.InvalidUTF8)
		}
	})
}
