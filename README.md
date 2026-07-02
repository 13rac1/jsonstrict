# jsonstrict

Go package that unmarshals JSON like `encoding/json`, but also reports unknown
fields, missing fields, duplicate keys, and invalid UTF-8. None of these cause
an error — callers decide what to do.

Standard `encoding/json` silently ignores unknown keys, doesn't report absent
ones, keeps the last of any duplicated key, and replaces invalid UTF-8 with
U+FFFD. jsonstrict surfaces all four.

If you only need to reject unknown fields without knowing which ones,
use `json.Decoder.DisallowUnknownFields` from the standard library instead.

## Install

```
go get github.com/13rac1/jsonstrict
```

## Usage

```go
var config Config
result, err := jsonstrict.Unmarshal(data, &config)
if err != nil {
    return err
}
for key, raw := range result.Unknown {
    log.Printf("unexpected field %s: %s", key, raw)
}
if len(result.Missing) > 0 {
    log.Printf("missing fields: %v", result.Missing)
}
if len(result.Duplicates) > 0 {
    log.Printf("duplicate keys: %v", result.Duplicates)
}
if len(result.InvalidUTF8) > 0 {
    log.Printf("invalid UTF-8 in: %v", result.InvalidUTF8)
}
```

`result.Unknown` is a `map[string]json.RawMessage` — each unknown field's raw
JSON value is preserved for inspection or further decoding. `Missing`,
`Duplicates`, and `InvalidUTF8` are `[]string` path lists, sorted.

## Paths

All diagnostics report paths in **bracket notation**, with no leading root, so
a path pastes directly onto a decoded value in Python (`json.loads`) or
JavaScript (`JSON.parse`) — `data["items"][0]["name"]`:

```
unknown:     ["address"]["zipp"], ["items"][0]["legacy_id"], ["config"]["dev"]["debug"]
missing:     ["address"]["zip"], ["items"][1]["name"]
duplicates:  ["role"], ["config"]["dev"]["host"]
invalidUTF8: ["name"], ["items"][2]["label"]
```

Object keys are JSON-quoted (so keys containing `.` or `[` stay unambiguous),
and slice, array, and map elements use an index or quoted key.

## What is checked

**Unknown and missing** are *schema* diagnostics — they describe the JSON
against your Go type, so checking is recursive into nested structs, slices,
arrays, and maps. Types that implement `json.Unmarshaler` (such as `time.Time`)
decode themselves, so they are opaque and never recursed into; if the target
type itself implements `json.Unmarshaler`, no unknown or missing fields are
reported.

**Duplicate keys and invalid UTF-8** are *structural* diagnostics — they
describe the input bytes, so they are reported for the whole input regardless
of the schema, including inside unknown or opaque values. A duplicate is any
key appearing more than once in its object (`encoding/json` keeps the last).
Invalid UTF-8 is reported for any object key or string value containing bad
bytes (`encoding/json` replaces them with U+FFFD). Invalid *escapes* such as a
lone surrogate `\uDEAD` are a different concern and are not reported.

Key matching is case-sensitive, unlike `encoding/json` which falls back to a
case-insensitive match. Given a field tagged `json:"name"`, the input
`{"Name":"bob"}` decodes into the field, but jsonstrict reports `["Name"]` as
unknown and `["name"]` as missing — so hard-strict callers using `result.Err()`
will reject payloads that differ only in key case. Strict means exact match.

Fields tagged with `omitempty` or `omitzero` are not reported as missing.
A key present with a JSON `null` value counts as present, not missing.

Callers who want hard strictness can reject a payload in one step with
`result.Err()`, which returns a `*jsonstrict.ResultError` listing the unknown,
missing, duplicate, and invalid-UTF-8 paths (or nil when there are none):

```go
result, err := jsonstrict.Unmarshal(data, &config)
if err == nil {
    err = result.Err()
}
```

The target must be a non-nil pointer to a struct.

## Performance

The JSON input is read three times: once to identify unknown and missing keys,
once by a token scan for duplicate keys and invalid UTF-8, and once to populate
the struct. Nested containers are re-parsed as they are walked, so expect
roughly 3x the CPU and memory of a plain `json.Unmarshal` for flat structs and
more for deeply nested payloads — run `go test -bench=.` for numbers on your
hardware. The UTF-8 scan is skipped when the whole input is already valid
UTF-8, and known field names are cached per struct type, so the reflection walk
costs nothing after the first call. Callers processing large payloads should
bound input size before calling `Unmarshal`.

## License

Apache 2.0
