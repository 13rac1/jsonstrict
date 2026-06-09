// Package jsonstrict provides JSON unmarshaling that detects unknown and
// missing fields.
//
// Standard encoding/json silently ignores unknown JSON keys and does not
// report absent ones. This package returns both alongside the decoded value,
// letting callers decide how to handle unexpected or incomplete data.
//
// If you only need to reject unknown fields without knowing which ones,
// use json.Decoder.DisallowUnknownFields from the standard library instead.
//
// Returned unknown field names are untrusted, attacker-controlled strings
// from the JSON input. Callers should sanitize them before logging or
// including in error responses.
//
// Note: encoding/json has complex rules for resolving conflicting field names
// across embedded structs at the same depth (both fields are ignored). This
// package does not replicate those rules — conflicting embedded fields are
// treated as known. This is a rare edge case in practice.
package jsonstrict

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// Result holds the field-level diagnostics from Unmarshal.
type Result struct {
	Unknown []string // JSON keys not matching any struct field
	Missing []string // struct fields not present in JSON
}

// Unmarshal unmarshals data into v and returns a Result indicating which JSON
// keys were unknown and which struct fields were missing from the input.
// Neither unknown nor missing fields cause an error — only the normal
// json.Unmarshal error (if any) is returned.
//
// v must be a non-nil pointer to a struct; any other value returns a
// *json.InvalidUnmarshalError.
//
// The data is parsed twice: once into a raw map to identify unknown and
// missing keys, then into v for the actual decode. encoding/json provides
// no hook to intercept unknown fields without erroring, so the double pass
// is intentional. Callers processing large payloads should bound input size.
func Unmarshal(data []byte, v any) (Result, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return Result{}, &json.InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}
	rt := rv.Type().Elem()
	if rt.Kind() != reflect.Struct {
		return Result{}, &json.InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}

	var result Result
	var raw map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &raw); jsonErr == nil {
		known := knownJSONKeys(rt)
		for key := range raw {
			if _, ok := known[key]; !ok {
				result.Unknown = append(result.Unknown, key)
			}
		}
		for key := range known {
			if _, ok := raw[key]; !ok {
				result.Missing = append(result.Missing, key)
			}
		}
		sort.Strings(result.Unknown)
		sort.Strings(result.Missing)
	}

	return result, json.Unmarshal(data, v)
}

// knownJSONKeys returns the set of JSON field names declared by t's struct
// tags. It recurses into anonymous (embedded) struct fields. Unexported and
// json:"-" fields are excluded. Tag options (e.g. ",omitempty") are stripped.
// Untagged exported fields fall back to the Go field name, matching
// encoding/json behavior.
func knownJSONKeys(t reflect.Type) map[string]struct{} {
	keys := make(map[string]struct{})
	for i := range t.NumField() {
		field := t.Field(i)

		if field.Anonymous {
			ft := field.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k := range knownJSONKeys(ft) {
					keys[k] = struct{}{}
				}
				continue
			}
		}

		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		keys[name] = struct{}{}
	}
	return keys
}
