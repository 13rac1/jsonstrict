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
// Key matching is case-sensitive, unlike encoding/json which matches
// case-insensitively. A JSON key "Name" for a field tagged json:"name"
// is reported as unknown even though encoding/json populates the struct
// from it. This is intentional — strict means exact match only.
//
// Returned unknown field names and values are untrusted, attacker-controlled
// data from the JSON input. Callers should sanitize them before logging or
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
	Unknown map[string]json.RawMessage // unknown JSON keys → raw values
	Missing []string                   // struct fields not present in JSON, sorted
}

// InvalidTargetError describes an invalid target passed to Unmarshal: a
// non-nil pointer whose element type is not a struct. Nil and non-pointer
// targets return *json.InvalidUnmarshalError instead, matching encoding/json.
type InvalidTargetError struct {
	Type reflect.Type // the target's type, e.g. *int
}

func (e *InvalidTargetError) Error() string {
	return "jsonstrict: Unmarshal(non-struct " + e.Type.String() + ")"
}

// Unmarshal unmarshals data into v and returns a Result indicating which JSON
// keys were unknown (with their raw values) and which struct fields were
// missing from the input. Neither unknown nor missing fields cause an error —
// only the normal json.Unmarshal error (if any) is returned.
//
// v must be a non-nil pointer to a struct. A nil or non-pointer v returns a
// *json.InvalidUnmarshalError, as encoding/json would; a non-nil pointer to
// a non-struct returns an *InvalidTargetError.
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
		return Result{}, &InvalidTargetError{Type: reflect.TypeOf(v)}
	}

	var result Result
	var raw map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &raw); jsonErr == nil {
		required, optional := knownJSONKeys(rt)
		for key, val := range raw {
			_, inRequired := required[key]
			_, inOptional := optional[key]
			if !inRequired && !inOptional {
				if result.Unknown == nil {
					result.Unknown = make(map[string]json.RawMessage)
				}
				result.Unknown[key] = val
			}
		}
		for key := range required {
			if _, ok := raw[key]; !ok {
				result.Missing = append(result.Missing, key)
			}
		}
		sort.Strings(result.Missing)
	}

	return result, json.Unmarshal(data, v)
}

// knownJSONKeys returns two sets of JSON field names declared by t's struct
// tags: required fields and optional fields. Fields tagged with omitempty are
// optional. Unexported and json:"-" fields are excluded. Untagged exported
// fields fall back to the Go field name.
//
// Anonymous (embedded) struct fields follow encoding/json rules: a json:"-"
// tag excludes the embedded struct entirely, a tag name makes it a regular
// named field, and only untagged embedded structs are flattened. Flattening
// tracks visited types so self-referential embedding cannot recurse forever.
func knownJSONKeys(t reflect.Type) (required, optional map[string]struct{}) {
	required = make(map[string]struct{})
	optional = make(map[string]struct{})
	addKnownJSONKeys(t, required, optional, map[reflect.Type]bool{t: true})
	return required, optional
}

func addKnownJSONKeys(t reflect.Type, required, optional map[string]struct{}, visited map[reflect.Type]bool) {
	for i := range t.NumField() {
		field := t.Field(i)

		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")

		if field.Anonymous && name == "" {
			ft := field.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if !visited[ft] {
					visited[ft] = true
					addKnownJSONKeys(ft, required, optional, visited)
				}
				continue
			}
		}

		if !field.IsExported() {
			continue
		}

		if name == "" {
			name = field.Name
		}
		if strings.Contains(opts, "omitempty") {
			optional[name] = struct{}{}
		} else {
			required[name] = struct{}{}
		}
	}
}
