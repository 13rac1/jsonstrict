package jsonstrict_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/13rac1/jsonstrict"
)

// testStruct is the target for most tests.
type testStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// embeddedParent embeds embeddedInner to test embedded struct handling.
type embeddedInner struct {
	InnerField string `json:"inner_field"`
}

type embeddedParent struct {
	embeddedInner
	Outer string `json:"outer"`
}

// dashStruct has a field tagged json:"-" that should be excluded.
type dashStruct struct {
	Visible string `json:"visible"`
	Hidden  string `json:"-"`
}

// omitemptyStruct has a field with the omitempty option.
type omitemptyStruct struct {
	Field string `json:"field,omitempty"`
}

// omitzeroStruct has a field with the omitzero option (Go 1.24+).
type omitzeroStruct struct {
	Field int `json:"field,omitzero"`
}

// shadowInner and shadowParent declare the same JSON name "x" at different
// embedding depths; encoding/json resolves to the shallower parent field.
type shadowInner struct {
	InnerX string `json:"x"`
}

type shadowParent struct {
	shadowInner
	OuterX string `json:"x,omitempty"`
}

// invalidTagStruct has a tag name encoding/json rejects (single quote is not
// an allowed character), so the Go field name is used instead.
type invalidTagStruct struct {
	Field string `json:"bad'name"`
}

// untaggedStruct has a field with no json tag (falls back to Go name).
type untaggedStruct struct {
	GoName string
}

// unexportedStruct has an unexported field that encoding/json ignores.
type unexportedStruct struct {
	Visible string `json:"visible"`
	secret  string //nolint:unused // intentionally unexported for testing
}

// PtrEmbeddedInner is the target for pointer-embedded struct tests.
// Exported because encoding/json requires embedded pointer targets to be
// exported when used from external test packages.
type PtrEmbeddedInner struct {
	Deep string `json:"deep"`
}

type ptrEmbeddedParent struct {
	*PtrEmbeddedInner
	Top string `json:"top"`
}

// TaggedEmbedInner is embedded WITH a json tag, so encoding/json treats it as
// a regular named field rather than flattening it. Exported because
// encoding/json requires embedded targets to be exported when decoded from
// external test packages.
type TaggedEmbedInner struct {
	A string `json:"a"`
}

type taggedEmbedParent struct {
	TaggedEmbedInner `json:"inner"`
	B                string `json:"b"`
}

// dashEmbedParent embeds a struct tagged json:"-", which encoding/json
// excludes entirely — its fields must not be treated as known.
type dashEmbedParent struct {
	embeddedInner `json:"-"`
	B             string `json:"b"`
}

// RecursiveNode embeds a pointer to itself; key collection must not loop.
type RecursiveNode struct {
	*RecursiveNode
	X int `json:"x"`
}

// RecursiveA and RecursiveB embed pointers to each other (an indirect cycle).
type RecursiveA struct {
	*RecursiveB
	AField string `json:"a_field"`
}

type RecursiveB struct {
	*RecursiveA
	BField string `json:"b_field"`
}

func TestUnmarshal_NoUnknownFields(t *testing.T) {
	var v testStruct
	result, err := jsonstrict.Unmarshal([]byte(`{"name":"alice","value":42}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "alice" || v.Value != 42 {
		t.Errorf("decode wrong: got %+v", v)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("expected no unknown fields, got %v", result.Unknown)
	}
	if len(result.Missing) != 0 {
		t.Errorf("expected no missing fields, got %v", result.Missing)
	}
}

func TestUnmarshal_UnknownFields(t *testing.T) {
	var v testStruct
	result, err := jsonstrict.Unmarshal([]byte(`{"name":"bob","value":1,"extra":"x"}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "bob" {
		t.Errorf("decode wrong: got %+v", v)
	}
	if len(result.Unknown) != 1 {
		t.Fatalf("expected 1 unknown field, got %d", len(result.Unknown))
	}
	if string(result.Unknown["extra"]) != `"x"` {
		t.Errorf("expected raw value '\"x\"', got %s", result.Unknown["extra"])
	}
	if len(result.Missing) != 0 {
		t.Errorf("expected no missing fields, got %v", result.Missing)
	}
}

func TestUnmarshal_MultipleUnknownFields(t *testing.T) {
	var v testStruct
	data := `{"name":"c","value":0,"a":"1","b":"2","c":"3"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 3 {
		t.Fatalf("expected 3 unknown fields, got %d", len(result.Unknown))
	}
	for _, key := range []string{"a", "b", "c"} {
		if _, ok := result.Unknown[key]; !ok {
			t.Errorf("expected unknown field %q", key)
		}
	}
}

func TestUnmarshal_UnknownFieldValues(t *testing.T) {
	var v testStruct
	data := `{"name":"a","value":1,"num":42,"obj":{"nested":true},"arr":[1,2]}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Unknown["num"]) != "42" {
		t.Errorf("expected raw 42, got %s", result.Unknown["num"])
	}
	if string(result.Unknown["obj"]) != `{"nested":true}` {
		t.Errorf("expected raw object, got %s", result.Unknown["obj"])
	}
	if string(result.Unknown["arr"]) != `[1,2]` {
		t.Errorf("expected raw array, got %s", result.Unknown["arr"])
	}
}

func TestUnmarshal_MissingFields(t *testing.T) {
	var v testStruct
	result, err := jsonstrict.Unmarshal([]byte(`{"name":"alice"}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "alice" {
		t.Errorf("decode wrong: got %+v", v)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("expected no unknown fields, got %v", result.Unknown)
	}
	if !slices.Equal(result.Missing, []string{"value"}) {
		t.Errorf("expected [value], got %v", result.Missing)
	}
}

func TestUnmarshal_AllFieldsMissing(t *testing.T) {
	var v testStruct
	result, err := jsonstrict.Unmarshal([]byte(`{}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(result.Missing, []string{"name", "value"}) {
		t.Errorf("expected [name value], got %v", result.Missing)
	}
}

func TestUnmarshal_InvalidJSON(t *testing.T) {
	var v testStruct
	result, err := jsonstrict.Unmarshal([]byte(`not json`), &v)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if len(result.Unknown) != 0 {
		t.Errorf("should not report unknown fields on invalid JSON, got %v", result.Unknown)
	}
	if len(result.Missing) != 0 {
		t.Errorf("should not report missing fields on invalid JSON, got %v", result.Missing)
	}
}

func TestUnmarshal_EmbeddedStruct(t *testing.T) {
	var v embeddedParent
	data := `{"inner_field":"i","outer":"o"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("embedded fields should be known, got %v", result.Unknown)
	}
	if len(result.Missing) != 0 {
		t.Errorf("expected no missing fields, got %v", result.Missing)
	}
	if v.InnerField != "i" || v.Outer != "o" {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_DashExcluded(t *testing.T) {
	var v dashStruct
	// "Hidden" is the Go field name, which would be the fallback if not tagged "-".
	// Since it IS tagged "-", "Hidden" in JSON should be unknown.
	data := `{"visible":"v","Hidden":"h"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 1 {
		t.Fatalf("expected 1 unknown field, got %d", len(result.Unknown))
	}
	if _, ok := result.Unknown["Hidden"]; !ok {
		t.Errorf("expected unknown field 'Hidden', got %v", result.Unknown)
	}
}

func TestUnmarshal_OmitemptyKnown(t *testing.T) {
	var v omitemptyStruct
	data := `{"field":"val"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("field with omitempty should be known, got %v", result.Unknown)
	}
}

func TestUnmarshal_OmitemptyNotMissing(t *testing.T) {
	var v omitemptyStruct
	result, err := jsonstrict.Unmarshal([]byte(`{}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Missing) != 0 {
		t.Errorf("omitempty field should not be missing, got %v", result.Missing)
	}
}

func TestUnmarshal_OmitzeroNotMissing(t *testing.T) {
	var v omitzeroStruct
	result, err := jsonstrict.Unmarshal([]byte(`{}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Missing) != 0 {
		t.Errorf("omitzero field should not be missing, got %v", result.Missing)
	}
}

func TestUnmarshal_NullValueIsPresent(t *testing.T) {
	var v testStruct
	// A key present with a null value counts as present, not missing —
	// presence is about the key, not the value.
	result, err := jsonstrict.Unmarshal([]byte(`{"name":null,"value":1}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Missing) != 0 {
		t.Errorf("null-valued key should count as present, got missing %v", result.Missing)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("expected no unknown fields, got %v", result.Unknown)
	}
	if v.Name != "" || v.Value != 1 {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_TopLevelNull(t *testing.T) {
	// Top-level null decodes as a no-op with no error (like encoding/json)
	// and reports every required field as missing.
	v := testStruct{Name: "keep", Value: 7}
	result, err := jsonstrict.Unmarshal([]byte(`null`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "keep" || v.Value != 7 {
		t.Errorf("null should leave the struct untouched, got %+v", v)
	}
	if !slices.Equal(result.Missing, []string{"name", "value"}) {
		t.Errorf("expected [name value] missing, got %v", result.Missing)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("expected no unknown fields, got %v", result.Unknown)
	}
}

func TestUnmarshal_ShadowedFieldShallowerWins(t *testing.T) {
	var v shadowParent
	// "x" is declared required at depth 1 and optional at depth 0;
	// encoding/json resolves to the shallower field, so it is optional.
	result, err := jsonstrict.Unmarshal([]byte(`{}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Missing) != 0 {
		t.Errorf("shadowed 'x' should resolve to the optional outer field, got missing %v", result.Missing)
	}

	// And the decode goes to the shallower field, matching the diagnostics.
	result, err = jsonstrict.Unmarshal([]byte(`{"x":"v"}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("expected no unknown fields, got %v", result.Unknown)
	}
	if v.OuterX != "v" || v.InnerX != "" {
		t.Errorf("expected shallower field populated, got %+v", v)
	}
}

func TestUnmarshal_InvalidTagNameFallsBack(t *testing.T) {
	var v invalidTagStruct
	// encoding/json rejects "bad'name" as a tag name and uses the Go field
	// name, so "Field" is known and "bad'name" is unknown.
	data := `{"Field":"v","bad'name":"w"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Unknown["bad'name"]; !ok {
		t.Errorf("invalid tag name should not be known, got %v", result.Unknown)
	}
	if len(result.Missing) != 0 {
		t.Errorf("expected no missing fields, got %v", result.Missing)
	}
	if v.Field != "v" {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_UntaggedField(t *testing.T) {
	var v untaggedStruct
	data := `{"GoName":"val"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("untagged field should use Go name, got %v", result.Unknown)
	}
	if v.GoName != "val" {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_UnexportedFieldIsUnknown(t *testing.T) {
	var v unexportedStruct
	// "secret" matches the unexported field name, but encoding/json ignores
	// unexported fields, so it should be reported as unknown.
	data := `{"visible":"v","secret":"s"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 1 {
		t.Fatalf("expected 1 unknown field, got %d", len(result.Unknown))
	}
	if _, ok := result.Unknown["secret"]; !ok {
		t.Errorf("expected unknown field 'secret', got %v", result.Unknown)
	}
}

func TestUnmarshal_PtrEmbeddedStruct(t *testing.T) {
	var v ptrEmbeddedParent
	data := `{"deep":"d","top":"t"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("ptr-embedded fields should be known, got %v", result.Unknown)
	}
	if v.Deep != "d" || v.Top != "t" {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_TaggedEmbeddedIsNamedField(t *testing.T) {
	var v taggedEmbedParent
	data := `{"inner":{"a":"x"},"b":"y"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 {
		t.Errorf("tagged embedded field should be known as %q, got unknown %v", "inner", result.Unknown)
	}
	if len(result.Missing) != 0 {
		t.Errorf("expected no missing fields, got %v", result.Missing)
	}
	if v.A != "x" || v.B != "y" {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_TaggedEmbeddedNotFlattened(t *testing.T) {
	var v taggedEmbedParent
	// "a" lives inside "inner", not at the top level: encoding/json does not
	// flatten a tagged embedded struct, so top-level "a" is unknown and
	// "inner" is missing.
	data := `{"a":"x","b":"y"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Unknown["a"]; !ok {
		t.Errorf("expected top-level 'a' to be unknown, got %v", result.Unknown)
	}
	if !slices.Equal(result.Missing, []string{"inner"}) {
		t.Errorf("expected [inner] missing, got %v", result.Missing)
	}
	if v.A != "" {
		t.Errorf("top-level 'a' should not populate embedded field, got %+v", v)
	}
}

func TestUnmarshal_DashEmbeddedExcluded(t *testing.T) {
	var v dashEmbedParent
	// The embedded struct is tagged json:"-", so encoding/json ignores it
	// entirely: "inner_field" must be reported unknown, not treated as known.
	data := `{"inner_field":"i","b":"y"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Unknown["inner_field"]; !ok {
		t.Errorf("expected unknown field 'inner_field', got %v", result.Unknown)
	}
	if len(result.Missing) != 0 {
		t.Errorf("expected no missing fields, got %v", result.Missing)
	}
	if v.InnerField != "" {
		t.Errorf("dash-embedded field should not be populated, got %+v", v)
	}
}

func TestUnmarshal_RecursiveEmbedded(t *testing.T) {
	var v RecursiveNode
	result, err := jsonstrict.Unmarshal([]byte(`{"x":1}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 || len(result.Missing) != 0 {
		t.Errorf("expected no diagnostics, got unknown=%v missing=%v", result.Unknown, result.Missing)
	}
	if v.X != 1 {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_MutuallyRecursiveEmbedded(t *testing.T) {
	var v RecursiveA
	data := `{"a_field":"a","b_field":"b"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 || len(result.Missing) != 0 {
		t.Errorf("expected no diagnostics, got unknown=%v missing=%v", result.Unknown, result.Missing)
	}
	if v.AField != "a" || v.BField != "b" {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_RepeatedCallsReturnFields(t *testing.T) {
	var v testStruct
	data := []byte(`{"name":"x","extra":"y"}`)

	// Each call must independently report unknown fields (no dedup).
	for i := range 3 {
		result, err := jsonstrict.Unmarshal(data, &v)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if len(result.Unknown) != 1 {
			t.Errorf("call %d: expected 1 unknown field, got %d", i, len(result.Unknown))
		}
		if _, ok := result.Unknown["extra"]; !ok {
			t.Errorf("call %d: expected unknown field 'extra', got %v", i, result.Unknown)
		}
	}
}

func TestUnmarshal_NilReturnsError(t *testing.T) {
	_, err := jsonstrict.Unmarshal([]byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for nil v")
	}
	var target *json.InvalidUnmarshalError
	if !errors.As(err, &target) {
		t.Errorf("expected *json.InvalidUnmarshalError, got %T: %v", err, err)
	}
}

func TestUnmarshal_NonPointerReturnsError(t *testing.T) {
	var v testStruct
	_, err := jsonstrict.Unmarshal([]byte(`{}`), v)
	if err == nil {
		t.Fatal("expected error for non-pointer v")
	}
	var target *json.InvalidUnmarshalError
	if !errors.As(err, &target) {
		t.Errorf("expected *json.InvalidUnmarshalError, got %T: %v", err, err)
	}
}

func TestUnmarshal_NonStructPointerReturnsError(t *testing.T) {
	v := new(int)
	_, err := jsonstrict.Unmarshal([]byte(`{}`), v)
	if err == nil {
		t.Fatal("expected error for non-struct pointer v")
	}
	var target *jsonstrict.InvalidTargetError
	if !errors.As(err, &target) {
		t.Fatalf("expected *jsonstrict.InvalidTargetError, got %T: %v", err, err)
	}
	// The message must not claim the pointer is nil (the old
	// *json.InvalidUnmarshalError rendered "json: Unmarshal(nil *int)").
	if got, want := err.Error(), "jsonstrict: Unmarshal(non-struct *int)"; got != want {
		t.Errorf("error message: got %q, want %q", got, want)
	}
}

// --- Nested checking ---

type nestedAddress struct {
	Street string `json:"street"`
	Zip    string `json:"zip"`
}

type nestedCustomer struct {
	Name    string        `json:"name"`
	Address nestedAddress `json:"address"`
}

type nestedPtrCustomer struct {
	Name    string         `json:"name"`
	Address *nestedAddress `json:"address"`
}

type nestedOptional struct {
	Address nestedAddress `json:"address,omitempty"`
}

type deepLevel3 struct {
	D string `json:"d"`
}

type deepLevel2 struct {
	L3 deepLevel3 `json:"l3"`
}

type deepLevel1 struct {
	L2 deepLevel2 `json:"l2"`
}

type sliceHolder struct {
	Items []nestedAddress `json:"items"`
}

type ptrSliceHolder struct {
	Items []*nestedAddress `json:"items"`
}

type arrayHolder struct {
	Pair [2]nestedAddress `json:"pair"`
}

type gridHolder struct {
	Grid [][]nestedAddress `json:"grid"`
}

type mapHolder struct {
	Config map[string]nestedAddress `json:"config"`
}

type timeHolder struct {
	Created time.Time `json:"created"`
	Name    string    `json:"name"`
}

type rawHolder struct {
	Raw  json.RawMessage `json:"raw"`
	Name string          `json:"name"`
}

type anyHolder struct {
	Meta map[string]any `json:"meta"`
	Free any            `json:"free"`
}

type nestedEmbedHolder struct {
	Wrapped embeddedParent `json:"wrapped"`
}

// opaqueTarget implements json.Unmarshaler; its JSON shape is unrelated to
// its Go fields, so it must never be recursed into.
type opaqueTarget struct {
	Sum int
}

func (o *opaqueTarget) UnmarshalJSON(b []byte) error {
	o.Sum = len(b)
	return nil
}

type opaqueHolder struct {
	Opaque opaqueTarget `json:"opaque"`
}

func TestUnmarshal_NestedUnknownAndMissing(t *testing.T) {
	var v nestedCustomer
	data := `{"name":"alice","address":{"street":"1 Main St","zipp":"90210"}}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 1 {
		t.Fatalf("expected 1 unknown field, got %v", result.Unknown)
	}
	if string(result.Unknown["address.zipp"]) != `"90210"` {
		t.Errorf("expected unknown address.zipp with raw value, got %v", result.Unknown)
	}
	if !slices.Equal(result.Missing, []string{"address.zip"}) {
		t.Errorf("expected [address.zip] missing, got %v", result.Missing)
	}
	if v.Name != "alice" || v.Address.Street != "1 Main St" {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_NestedPointerStruct(t *testing.T) {
	var v nestedPtrCustomer
	data := `{"name":"alice","address":{"street":"s","zipp":"z"}}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Unknown["address.zipp"]; !ok {
		t.Errorf("expected unknown address.zipp, got %v", result.Unknown)
	}
	if !slices.Equal(result.Missing, []string{"address.zip"}) {
		t.Errorf("expected [address.zip] missing, got %v", result.Missing)
	}
}

func TestUnmarshal_MissingNestedObjectReportedShallow(t *testing.T) {
	var v nestedCustomer
	result, err := jsonstrict.Unmarshal([]byte(`{}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the absent object itself, not every path beneath it.
	if !slices.Equal(result.Missing, []string{"address", "name"}) {
		t.Errorf("expected [address name] missing, got %v", result.Missing)
	}
}

func TestUnmarshal_DeeplyNested(t *testing.T) {
	var v deepLevel1
	data := `{"l2":{"l3":{"x":1}}}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Unknown["l2.l3.x"]; !ok {
		t.Errorf("expected unknown l2.l3.x, got %v", result.Unknown)
	}
	if !slices.Equal(result.Missing, []string{"l2.l3.d"}) {
		t.Errorf("expected [l2.l3.d] missing, got %v", result.Missing)
	}
}

func TestUnmarshal_NestedNullNotRecursed(t *testing.T) {
	var v nestedCustomer
	data := `{"name":"a","address":null}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 || len(result.Missing) != 0 {
		t.Errorf("null nested value must not be recursed, got unknown=%v missing=%v",
			result.Unknown, result.Missing)
	}
}

func TestUnmarshal_NestedWrongTypeNotRecursed(t *testing.T) {
	var v nestedCustomer
	data := `{"name":"a","address":"not an object"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err == nil {
		t.Fatal("expected decode error for wrong-shaped nested value")
	}
	if len(result.Unknown) != 0 || len(result.Missing) != 0 {
		t.Errorf("shape mismatch must surface as the decode error, not diagnostics; got unknown=%v missing=%v",
			result.Unknown, result.Missing)
	}
}

func TestUnmarshal_SliceOfStructs(t *testing.T) {
	var v sliceHolder
	data := `{"items":[{"street":"a","zip":"1"},{"street":"b","extra":true}]}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Unknown["items[1].extra"]) != "true" {
		t.Errorf("expected unknown items[1].extra, got %v", result.Unknown)
	}
	if !slices.Equal(result.Missing, []string{"items[1].zip"}) {
		t.Errorf("expected [items[1].zip] missing, got %v", result.Missing)
	}
}

func TestUnmarshal_SliceOfPointerStructs(t *testing.T) {
	var v ptrSliceHolder
	data := `{"items":[{"street":"a"}]}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(result.Missing, []string{"items[0].zip"}) {
		t.Errorf("expected [items[0].zip] missing, got %v", result.Missing)
	}
}

func TestUnmarshal_ArrayOfStructs(t *testing.T) {
	var v arrayHolder
	data := `{"pair":[{"street":"a","zip":"1"},{"street":"b","bogus":0}]}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Unknown["pair[1].bogus"]; !ok {
		t.Errorf("expected unknown pair[1].bogus, got %v", result.Unknown)
	}
	if !slices.Equal(result.Missing, []string{"pair[1].zip"}) {
		t.Errorf("expected [pair[1].zip] missing, got %v", result.Missing)
	}
}

func TestUnmarshal_NestedSliceOfSlices(t *testing.T) {
	var v gridHolder
	data := `{"grid":[[{"street":"a","zip":"1"}],[{"street":"b","zip":"2"},{"street":"c"}]]}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(result.Missing, []string{"grid[1][1].zip"}) {
		t.Errorf("expected [grid[1][1].zip] missing, got %v", result.Missing)
	}
}

func TestUnmarshal_MapOfStructs(t *testing.T) {
	var v mapHolder
	data := `{"config":{"dev":{"street":"s","debug":true},"a.b":{"street":"t","zip":"z"}}}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Map keys are quoted in paths, so keys containing dots stay unambiguous.
	if string(result.Unknown[`config["dev"].debug`]) != "true" {
		t.Errorf(`expected unknown config["dev"].debug, got %v`, result.Unknown)
	}
	if !slices.Equal(result.Missing, []string{`config["dev"].zip`}) {
		t.Errorf(`expected [config["dev"].zip] missing, got %v`, result.Missing)
	}
}

func TestUnmarshal_JSONUnmarshalerOpaque(t *testing.T) {
	var v opaqueHolder
	// opaqueTarget decodes itself; whatever shape its JSON takes, its
	// interior must produce no diagnostics.
	data := `{"opaque":{"anything":1,"goes":[true]}}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 || len(result.Missing) != 0 {
		t.Errorf("json.Unmarshaler types must be opaque, got unknown=%v missing=%v",
			result.Unknown, result.Missing)
	}
	if v.Opaque.Sum == 0 {
		t.Errorf("custom unmarshaler not invoked: %+v", v)
	}
}

func TestUnmarshal_TimeFieldOpaque(t *testing.T) {
	var v timeHolder
	data := `{"created":"2026-07-02T10:00:00Z","name":"a"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 || len(result.Missing) != 0 {
		t.Errorf("time.Time must be opaque, got unknown=%v missing=%v",
			result.Unknown, result.Missing)
	}
	if v.Created.IsZero() {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_RawMessageOpaque(t *testing.T) {
	var v rawHolder
	data := `{"raw":{"any":{"thing":1}},"name":"a"}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 || len(result.Missing) != 0 {
		t.Errorf("json.RawMessage must be opaque, got unknown=%v missing=%v",
			result.Unknown, result.Missing)
	}
	if string(v.Raw) != `{"any":{"thing":1}}` {
		t.Errorf("decode wrong: got %s", v.Raw)
	}
}

func TestUnmarshal_InterfaceFieldsNotRecursed(t *testing.T) {
	var v anyHolder
	data := `{"meta":{"a":{"b":1}},"free":{"c":2}}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 0 || len(result.Missing) != 0 {
		t.Errorf("interface-typed fields have no schema, got unknown=%v missing=%v",
			result.Unknown, result.Missing)
	}
}

func TestUnmarshal_NestedEmbedded(t *testing.T) {
	var v nestedEmbedHolder
	data := `{"wrapped":{"inner_field":"i","outer":"o","bogus":1}}`
	result, err := jsonstrict.Unmarshal([]byte(data), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Unknown) != 1 {
		t.Fatalf("expected 1 unknown field, got %v", result.Unknown)
	}
	if _, ok := result.Unknown["wrapped.bogus"]; !ok {
		t.Errorf("expected unknown wrapped.bogus, got %v", result.Unknown)
	}
	if len(result.Missing) != 0 {
		t.Errorf("expected no missing fields, got %v", result.Missing)
	}
	if v.Wrapped.InnerField != "i" || v.Wrapped.Outer != "o" {
		t.Errorf("decode wrong: got %+v", v)
	}
}

func TestUnmarshal_OptionalNestedStillChecked(t *testing.T) {
	var v nestedOptional
	// An optional nested object is never missing, but when present its
	// interior is still checked.
	result, err := jsonstrict.Unmarshal([]byte(`{}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Missing) != 0 {
		t.Errorf("optional nested object should not be missing, got %v", result.Missing)
	}

	result, err = jsonstrict.Unmarshal([]byte(`{"address":{"street":"s","zip":"z","x":1}}`), &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.Unknown["address.x"]; !ok {
		t.Errorf("expected unknown address.x, got %v", result.Unknown)
	}
}
