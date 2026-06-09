package jsonstrict_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

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
	if !slices.Equal(result.Unknown, []string{"extra"}) {
		t.Errorf("expected [extra], got %v", result.Unknown)
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
	if !slices.Equal(result.Unknown, []string{"a", "b", "c"}) {
		t.Errorf("expected [a b c], got %v", result.Unknown)
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
	if !slices.Equal(result.Unknown, []string{"Hidden"}) {
		t.Errorf("expected [Hidden], got %v", result.Unknown)
	}
}

func TestUnmarshal_OmitemptyStripped(t *testing.T) {
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
	if !slices.Equal(result.Unknown, []string{"secret"}) {
		t.Errorf("expected [secret], got %v", result.Unknown)
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

func TestUnmarshal_RepeatedCallsReturnFields(t *testing.T) {
	var v testStruct
	data := []byte(`{"name":"x","extra":"y"}`)

	// Each call must independently report unknown fields (no dedup).
	for i := range 3 {
		result, err := jsonstrict.Unmarshal(data, &v)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !slices.Equal(result.Unknown, []string{"extra"}) {
			t.Errorf("call %d: expected [extra], got %v", i, result.Unknown)
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
	var target *json.InvalidUnmarshalError
	if !errors.As(err, &target) {
		t.Errorf("expected *json.InvalidUnmarshalError, got %T: %v", err, err)
	}
}
