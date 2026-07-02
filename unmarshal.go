// Package jsonstrict provides JSON unmarshaling that detects unknown fields,
// missing fields, duplicate keys, and invalid UTF-8.
//
// Standard encoding/json silently ignores unknown JSON keys, does not report
// absent ones, keeps the last of any duplicated keys, and replaces invalid
// UTF-8 with U+FFFD. This package returns all four diagnostics alongside the
// decoded value, letting callers decide how to handle unexpected, incomplete,
// or malformed data.
//
// Diagnostics come in two kinds. Unknown and missing fields are schema
// diagnostics: they describe how the JSON relates to the Go type, so checking
// is recursive over struct, pointer, slice, array, and map types when the JSON
// value has the matching shape. Duplicate keys and invalid UTF-8 are structural
// diagnostics: they describe the input bytes and are reported for the entire
// input, independent of the schema.
//
// All diagnostics report paths in bracket notation: object keys are JSON-quoted
// (["address"]["zip"]) and slice, array, and map elements use an index or
// quoted key (["items"][0]["name"], ["config"]["dev"]["host"]). There is no
// leading root, so a path pastes directly onto a decoded value in Python or
// JavaScript: data["items"][0]["name"].
//
// Types implementing json.Unmarshaler, such as time.Time, decode themselves, so
// the schema pass treats them as opaque and never recurses into them —
// including the target type itself; the same goes for interface-typed fields,
// which have no schema. Null values are never recursed into, and a missing
// nested object is reported by its own path only, not by every path beneath it.
// The structural pass is unaffected by opacity: duplicate keys and invalid
// UTF-8 inside an opaque or unknown value are still reported.
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
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Result holds the field-level diagnostics from Unmarshal.
//
// Paths use bracket notation: object keys are JSON-quoted (["address"]["zip"])
// and array elements use an index (["items"][0]), with no leading root, so a
// path pastes directly onto a decoded value in Python or JavaScript
// (data["items"][0]["name"]).
//
// Unknown and Missing are schema diagnostics: they describe how the JSON
// relates to the Go type. Duplicates and InvalidUTF8 are structural
// diagnostics: they describe the input bytes and are reported for the whole
// input regardless of the target type (see the note on opaque targets in the
// Unmarshal doc).
type Result struct {
	// Unknown maps the path of each unknown JSON key to its raw value.
	// It is nil when there are no unknown fields.
	Unknown map[string]json.RawMessage
	// Missing lists the paths of required struct fields absent from the
	// JSON, sorted lexicographically.
	Missing []string
	// Duplicates lists the paths of JSON keys that appear more than once in
	// their enclosing object, sorted lexicographically. encoding/json keeps
	// the last such value silently; each duplicated key is listed once. It is
	// nil when there are no duplicate keys.
	Duplicates []string
	// InvalidUTF8 lists the paths of JSON strings — object keys or string
	// values — that contain invalid UTF-8 bytes, sorted lexicographically.
	// encoding/json silently replaces those bytes with U+FFFD. A bad key is
	// reported at its own slot, using the U+FFFD-decoded key in the path. It is
	// nil when all strings are valid UTF-8.
	InvalidUTF8 []string
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
	if len(r.Unknown) == 0 && len(r.Missing) == 0 &&
		len(r.Duplicates) == 0 && len(r.InvalidUTF8) == 0 {
		return nil
	}
	e := &ResultError{
		Missing:     r.Missing,
		Duplicates:  r.Duplicates,
		InvalidUTF8: r.InvalidUTF8,
	}
	for path := range r.Unknown {
		e.Unknown = append(e.Unknown, path)
	}
	sort.Strings(e.Unknown)
	return e
}

// A ResultError is returned by Result.Err when a payload has unknown, missing,
// duplicate, or invalid-UTF-8 fields.
type ResultError struct {
	Unknown     []string // paths of unknown fields, sorted
	Missing     []string // paths of missing required fields, sorted
	Duplicates  []string // paths of duplicate keys, sorted
	InvalidUTF8 []string // paths of strings with invalid UTF-8, sorted
}

func (e *ResultError) Error() string {
	var b strings.Builder
	b.WriteString("jsonstrict:")
	written := false
	clause := func(label string, paths []string) {
		if len(paths) == 0 {
			return
		}
		if written {
			b.WriteString(";")
		}
		b.WriteString(" ")
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(strings.Join(paths, ", "))
		written = true
	}
	clause("unknown fields", e.Unknown)
	clause("missing fields", e.Missing)
	clause("duplicate fields", e.Duplicates)
	clause("invalid UTF-8", e.InvalidUTF8)
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

// Unmarshal unmarshals data into v and returns a Result reporting which JSON
// keys were unknown (with their raw values), which struct fields were missing,
// which keys were duplicated, and which strings held invalid UTF-8. None of
// these cause an error — only the normal json.Unmarshal error (if any) is
// returned. See Result.Err for a one-step strict check.
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
// If the target type itself implements json.Unmarshaler, it decodes itself and
// its struct tags say nothing about the JSON shape it accepts, so no unknown or
// missing fields are reported — just as such types are opaque when nested. The
// structural diagnostics (Duplicates, InvalidUTF8) are still reported, since
// they describe the input bytes rather than the schema.
//
// The data is read three times: once into raw form to identify unknown and
// missing keys (nested containers are re-parsed as they are walked), once by a
// token scan for duplicate keys and invalid UTF-8, and once into v for the
// actual decode. encoding/json provides no hook to intercept these conditions
// without erroring, so the extra passes are intentional. Callers processing
// large payloads should bound input size.
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
	result.Duplicates, result.InvalidUTF8 = scanRaw(data)

	return result, json.Unmarshal(data, v)
}

// checkStruct records unknown keys in raw and missing required fields of t.
// path is empty at the root, or the bracket path of the object being checked;
// each field's path is derived by appending its key.
func checkStruct(t reflect.Type, raw map[string]json.RawMessage, path string, result *Result) {
	known := knownJSONKeys(t)
	for key, val := range raw {
		info, ok := known[key]
		if !ok {
			if result.Unknown == nil {
				result.Unknown = make(map[string]json.RawMessage)
			}
			result.Unknown[appendKey(path, key)] = val
			continue
		}
		checkValue(info.typ, val, appendKey(path, key), result)
	}
	for key, info := range known {
		if info.optional {
			continue
		}
		if _, ok := raw[key]; !ok {
			result.Missing = append(result.Missing, appendKey(path, key))
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
		checkStruct(t, raw, path, result)
	case reflect.Slice, reflect.Array:
		var elems []json.RawMessage
		if err := json.Unmarshal(val, &elems); err != nil {
			return
		}
		for i, e := range elems {
			checkValue(t.Elem(), e, appendIndex(path, i), result)
		}
	case reflect.Map:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(val, &m); err != nil {
			return
		}
		for k, e := range m {
			checkValue(t.Elem(), e, appendKey(path, k), result)
		}
	default:
		// Scalars and interfaces have no schema to inspect.
	}
}

// appendKey extends a bracket path with an object key, e.g. appendKey(`["a"]`,
// "b") is `["a"]["b"]`. Keys are JSON-quoted so the result is unambiguous and
// pastes directly into Python or JavaScript subscripting.
func appendKey(prefix, key string) string {
	return prefix + "[" + strconv.Quote(key) + "]"
}

// appendIndex extends a bracket path with an array index, e.g.
// appendIndex(`["a"]`, 0) is `["a"][0]`.
func appendIndex(prefix string, i int) string {
	return prefix + "[" + strconv.Itoa(i) + "]"
}

// scanFrame tracks one open container during a rawScanner walk.
type scanFrame struct {
	isObject   bool
	expectKey  bool           // object: next string token is a key
	seen       map[string]int // object: key occurrence counts (lazy)
	index      int            // array: next element index
	path       string         // bracket path of this container
	pendingKey string         // object: key whose value comes next
}

// rawScanner walks raw JSON tokens, accumulating structural diagnostics.
type rawScanner struct {
	checkUTF8 bool // whether any string might hold invalid UTF-8
	stack     []scanFrame
	dups      []string
	badUTF8   []string
}

// scanRaw walks the raw JSON in data and reports structural anomalies the
// schema pass cannot see: keys appearing more than once within an object
// (encoding/json silently keeps the last value) and JSON strings — object keys
// or string values — containing invalid UTF-8 bytes (encoding/json silently
// substitutes U+FFFD). Paths use the same bracket notation as the schema
// diagnostics. The walk is schema-free, so it covers the whole input, including
// regions the schema pass reports as unknown or treats as opaque. It returns no
// diagnostics for malformed input, which the real decode reports instead.
func scanRaw(data []byte) (dups, badUTF8 []string) {
	// The overwhelmingly common case is all-valid UTF-8; check once up front so
	// per-string work is skipped entirely when there is nothing to find. Invalid
	// UTF-8 in valid JSON can only occur inside a string token.
	s := &rawScanner{checkUTF8: !utf8.Valid(data)}

	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		off0 := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil // malformed; the real decode reports it
		}
		s.token(tok, data[off0:dec.InputOffset()])
	}

	sort.Strings(s.dups)
	return s.dups, sortUnique(s.badUTF8)
}

// token dispatches one JSON token; raw is its byte span (including any leading
// structural bytes, which are ASCII and so never affect a UTF-8 check).
func (s *rawScanner) token(tok json.Token, raw []byte) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			s.enter(true)
		case '[':
			s.enter(false)
		default: // '}' or ']'
			s.leave()
		}
	case string:
		s.str(t, raw)
	default:
		s.advance() // number, bool, or null value
	}
}

// enter pushes a container whose path is its value slot in the parent.
func (s *rawScanner) enter(isObject bool) {
	s.stack = append(s.stack, scanFrame{
		isObject:  isObject,
		expectKey: isObject,
		path:      s.valuePath(),
	})
}

// leave pops the current container and advances the parent past it.
func (s *rawScanner) leave() {
	s.stack = s.stack[:len(s.stack)-1]
	s.advance()
}

// str handles a string token as either an object key or a string value.
func (s *rawScanner) str(val string, raw []byte) {
	if top := s.top(); top != nil && top.isObject && top.expectKey {
		s.key(top, val, raw)
		return
	}
	if s.checkUTF8 && !utf8.Valid(raw) {
		s.badUTF8 = append(s.badUTF8, s.valuePath())
	}
	s.advance()
}

// key records an object key: its UTF-8 validity, its occurrence count (for
// duplicate detection), and that its value comes next.
func (s *rawScanner) key(top *scanFrame, val string, raw []byte) {
	if s.checkUTF8 && !utf8.Valid(raw) {
		s.badUTF8 = append(s.badUTF8, appendKey(top.path, val))
	}
	if top.seen == nil {
		top.seen = make(map[string]int)
	}
	top.seen[val]++
	if top.seen[val] == 2 {
		s.dups = append(s.dups, appendKey(top.path, val))
	}
	top.pendingKey = val
	top.expectKey = false
}

// top returns the current container frame, or nil at the top level.
func (s *rawScanner) top() *scanFrame {
	if n := len(s.stack); n > 0 {
		return &s.stack[n-1]
	}
	return nil
}

// advance moves the current container past a just-completed value.
func (s *rawScanner) advance() {
	if top := s.top(); top != nil {
		if top.isObject {
			top.expectKey = true
		} else {
			top.index++
		}
	}
}

// valuePath is the path of the value now being consumed or opened in the
// current container; empty for a top-level value.
func (s *rawScanner) valuePath() string {
	top := s.top()
	if top == nil {
		return ""
	}
	if top.isObject {
		return appendKey(top.path, top.pendingKey)
	}
	return appendIndex(top.path, top.index)
}

// sortUnique sorts s in place and returns it with adjacent duplicates removed.
func sortUnique(s []string) []string {
	if len(s) < 2 {
		return s
	}
	sort.Strings(s)
	out := s[:1]
	for _, v := range s[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
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
