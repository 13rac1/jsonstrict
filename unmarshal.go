// Package jsonstrict provides JSON unmarshaling that detects unknown and
// missing fields.
//
// Standard encoding/json silently ignores unknown JSON keys and does not
// report absent ones. This package returns both alongside the decoded value,
// letting callers decide how to handle unexpected or incomplete data.
//
// Checking is recursive. Fields of struct, pointer, slice, array, and map
// types are walked when the JSON value has the matching shape, and
// diagnostics report paths: object fields join with dots (address.zip),
// slice and array elements use an index (items[0].name), and map values use
// a quoted key (config["dev"].host). Types implementing json.Unmarshaler,
// such as time.Time, decode themselves, so they are treated as opaque and
// never recursed into — including the target type itself; the same goes for
// interface-typed fields, which have no schema. Null values are never recursed into, and a missing nested
// object is reported by its own path only, not by every path beneath it.
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
// Note: encoding/json resolves conflicting field names across embedded
// structs by depth — shallower fields shadow deeper ones — which this package
// follows. Its remaining rule (conflicts at the same depth cause both fields
// to be ignored) is not replicated: same-depth conflicts are treated as
// known. This is a rare edge case in practice.
package jsonstrict

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// Result holds the field-level diagnostics from Unmarshal.
type Result struct {
	// Unknown maps the path of each unknown JSON key to its raw value.
	// It is nil when there are no unknown fields.
	Unknown map[string]json.RawMessage
	// Missing lists the paths of required struct fields absent from the
	// JSON, sorted lexicographically.
	Missing []string
}

// Err returns a *ResultError describing the unknown and missing fields, or
// nil if there are none. It is a convenience for callers who want hard
// strictness:
//
//	result, err := jsonstrict.Unmarshal(data, &v)
//	if err == nil {
//		err = result.Err()
//	}
//
// The error message includes field names taken from the JSON input; see the
// package note on untrusted data before echoing it to clients or logs.
func (r Result) Err() error {
	if len(r.Unknown) == 0 && len(r.Missing) == 0 {
		return nil
	}
	e := &ResultError{Missing: r.Missing}
	for path := range r.Unknown {
		e.Unknown = append(e.Unknown, path)
	}
	sort.Strings(e.Unknown)
	return e
}

// A ResultError is returned by Result.Err when a payload has unknown or
// missing fields.
type ResultError struct {
	Unknown []string // paths of unknown fields, sorted
	Missing []string // paths of missing required fields, sorted
}

func (e *ResultError) Error() string {
	var b strings.Builder
	b.WriteString("jsonstrict:")
	if len(e.Unknown) > 0 {
		b.WriteString(" unknown fields: ")
		b.WriteString(strings.Join(e.Unknown, ", "))
	}
	if len(e.Missing) > 0 {
		if len(e.Unknown) > 0 {
			b.WriteString(";")
		}
		b.WriteString(" missing fields: ")
		b.WriteString(strings.Join(e.Missing, ", "))
	}
	return b.String()
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
// Missing means the key is absent from the JSON object: a key present with a
// null value counts as present. Fields tagged omitempty or omitzero are never
// reported as missing. A top-level JSON null input decodes as a no-op with no
// error (matching encoding/json) and reports every required field as missing.
//
// v must be a non-nil pointer to a struct. A nil or non-pointer v returns a
// *json.InvalidUnmarshalError, as encoding/json would; a non-nil pointer to
// a non-struct returns an *InvalidTargetError.
//
// If the target type itself implements json.Unmarshaler, it decodes itself
// and its struct tags say nothing about the JSON shape it accepts, so it is
// opaque — the Result is always empty — just as such types are when nested.
//
// The data is parsed twice: once into raw form to identify unknown and
// missing keys (nested containers are re-parsed as they are walked), then
// into v for the actual decode. encoding/json provides no hook to intercept
// unknown fields without erroring, so the double pass is intentional.
// Callers processing large payloads should bound input size.
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
	if !reflect.PointerTo(rt).Implements(jsonUnmarshalerType) {
		var raw map[string]json.RawMessage
		if jsonErr := json.Unmarshal(data, &raw); jsonErr == nil {
			checkStruct(rt, raw, "", &result)
			sort.Strings(result.Missing)
		}
	}

	return result, json.Unmarshal(data, v)
}

// checkStruct records unknown keys in raw and missing required fields of t.
// prefix is empty at the root, or the parent path including a trailing dot.
func checkStruct(t reflect.Type, raw map[string]json.RawMessage, prefix string, result *Result) {
	known := knownJSONKeys(t)
	for key, val := range raw {
		info, ok := known[key]
		if !ok {
			if result.Unknown == nil {
				result.Unknown = make(map[string]json.RawMessage)
			}
			result.Unknown[prefix+key] = val
			continue
		}
		checkValue(info.typ, val, prefix+key, result)
	}
	for key, info := range known {
		if info.optional {
			continue
		}
		if _, ok := raw[key]; !ok {
			result.Missing = append(result.Missing, prefix+key)
		}
	}
}

// checkValue recurses into a known field's value when its Go type has an
// inspectable schema and the JSON value has the matching shape. Types
// implementing json.Unmarshaler decode themselves, so they are opaque.
// Values that fail to parse as the expected shape (including null) are
// skipped — a shape mismatch surfaces as the decode error from
// encoding/json, not as diagnostics.
func checkValue(t reflect.Type, val json.RawMessage, path string, result *Result) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(jsonUnmarshalerType) {
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(val, &raw); err != nil || raw == nil {
			return
		}
		checkStruct(t, raw, path+".", result)
	case reflect.Slice, reflect.Array:
		var elems []json.RawMessage
		if err := json.Unmarshal(val, &elems); err != nil {
			return
		}
		for i, e := range elems {
			checkValue(t.Elem(), e, path+"["+strconv.Itoa(i)+"]", result)
		}
	case reflect.Map:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(val, &m); err != nil {
			return
		}
		for k, e := range m {
			checkValue(t.Elem(), e, path+"["+strconv.Quote(k)+"]", result)
		}
	default:
		// Scalars and interfaces have no schema to inspect.
	}
}

var jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()

// fieldInfo records how a known JSON key was declared, for conflict
// resolution and missing detection.
type fieldInfo struct {
	typ      reflect.Type // field type, for recursing into nested values
	optional bool         // omitempty or omitzero: never reported missing
	tagged   bool         // name came from a json tag, not the Go field name
	depth    int          // embedding depth; shallower shadows deeper
}

// knownJSONKeys returns the JSON field names declared by t's struct tags,
// mapped to how each was declared. Fields tagged with omitempty or omitzero
// are optional. Unexported and json:"-" fields are excluded. Untagged
// exported fields fall back to the Go field name, as do fields whose tag
// name encoding/json would reject as invalid.
//
// Anonymous (embedded) struct fields follow encoding/json rules: a json:"-"
// tag excludes the embedded struct entirely, a tag name makes it a regular
// named field, and only untagged embedded structs are flattened. Flattening
// tracks visited types so self-referential embedding cannot recurse forever.
// Name conflicts resolve as encoding/json does — see shadows.
//
// Results are cached per type, so the reflection walk runs once per struct
// type for the life of the process. Callers must not mutate the returned map.
func knownJSONKeys(t reflect.Type) map[string]fieldInfo {
	if cached, ok := fieldCache.Load(t); ok {
		if fields, ok := cached.(map[string]fieldInfo); ok {
			return fields
		}
	}
	fields := make(map[string]fieldInfo)
	addKnownJSONKeys(t, fields, 0, map[reflect.Type]bool{t: true})
	if actual, loaded := fieldCache.LoadOrStore(t, fields); loaded {
		if cached, ok := actual.(map[string]fieldInfo); ok {
			return cached
		}
	}
	return fields
}

// fieldCache maps reflect.Type → map[string]fieldInfo, mirroring the field
// cache in encoding/json.
var fieldCache sync.Map

func addKnownJSONKeys(t reflect.Type, fields map[string]fieldInfo, depth int, visited map[reflect.Type]bool) {
	for i := range t.NumField() {
		field := t.Field(i)

		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if !isValidTag(name) {
			name = ""
		}

		if field.Anonymous && name == "" {
			ft := field.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if !visited[ft] {
					visited[ft] = true
					addKnownJSONKeys(ft, fields, depth+1, visited)
				}
				continue
			}
		}

		if !field.IsExported() {
			continue
		}

		info := fieldInfo{
			typ:      field.Type,
			optional: tagOptContains(opts, "omitempty") || tagOptContains(opts, "omitzero"),
			tagged:   name != "",
			depth:    depth,
		}
		if name == "" {
			name = field.Name
		}
		if existing, ok := fields[name]; ok && !shadows(info, existing) {
			continue
		}
		fields[name] = info
	}
}

// shadows reports whether candidate wins over existing for the same JSON
// name, following encoding/json: shallower embedding depth wins, and at equal
// depth a tagged field beats an untagged one. Remaining same-depth ties keep
// the first field seen (encoding/json ignores both; see the package doc).
func shadows(candidate, existing fieldInfo) bool {
	if candidate.depth != existing.depth {
		return candidate.depth < existing.depth
	}
	return candidate.tagged && !existing.tagged
}

// tagOptContains reports whether the comma-separated tag options contain opt,
// matching whole options only, like encoding/json's tagOptions.Contains.
func tagOptContains(opts, opt string) bool {
	for opts != "" {
		var o string
		o, opts, _ = strings.Cut(opts, ",")
		if o == opt {
			return true
		}
	}
	return false
}

// isValidTag mirrors encoding/json's tag-name validation: letters, digits,
// and a limited set of punctuation. Names it rejects fall back to the Go
// field name.
func isValidTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
			// Backslash and quote chars are reserved, but otherwise any
			// punctuation is fine.
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}
