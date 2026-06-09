// Package jsonstrict provides JSON unmarshaling that detects unknown fields.
//
// Standard encoding/json silently ignores unknown JSON keys. This package
// returns the names of unknown fields alongside the decoded value, letting
// callers decide how to handle unexpected data.
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

// Unmarshal unmarshals data into v and returns the names of any JSON keys
// not represented by a json struct tag on v's type. Unknown fields never cause
// an error — only the normal json.Unmarshal error (if any) is returned.
//
// v must be a non-nil pointer to a struct; any other value returns a
// *json.InvalidUnmarshalError.
//
// The data is parsed twice: once into a raw map to identify unknown keys,
// then into v for the actual decode. encoding/json provides no hook to
// intercept unknown fields without erroring, so the double pass is
// intentional. Callers processing large payloads should bound input size.
func Unmarshal(data []byte, v any) (unknownFields []string, err error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil, &json.InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}
	rt := rv.Type().Elem()
	if rt.Kind() != reflect.Struct {
		return nil, &json.InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}

	var raw map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &raw); jsonErr == nil {
		known := knownJSONKeys(rt)
		for key := range raw {
			if _, ok := known[key]; !ok {
				unknownFields = append(unknownFields, key)
			}
		}
		sort.Strings(unknownFields)
	}

	return unknownFields, json.Unmarshal(data, v)
}

// knownJSONKeys returns the set of JSON field names declared by t's struct tags.
func knownJSONKeys(t reflect.Type) map[string]struct{} {
	keys := make(map[string]struct{})
	collectJSONKeys(t, keys)
	return keys
}

// collectJSONKeys recurses into t's fields and adds their JSON names to keys.
// It recurses into anonymous (embedded) struct fields. Unexported and
// json:"-" fields are excluded. Tag options (e.g. ",omitempty") are stripped.
// Untagged exported fields fall back to the Go field name, matching
// encoding/json behavior.
func collectJSONKeys(t reflect.Type, keys map[string]struct{}) {
	for i := range t.NumField() {
		field := t.Field(i)

		if field.Anonymous {
			ft := field.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectJSONKeys(ft, keys)
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
}
