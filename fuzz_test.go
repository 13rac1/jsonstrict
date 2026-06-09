package jsonstrict_test

import (
	"testing"

	"github.com/13rac1/jsonstrict"
)

// fuzzTarget is a minimal struct for exercising Unmarshal.
type fuzzTarget struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
	OK    bool   `json:"ok"`
}

func FuzzUnmarshal(f *testing.F) {
	f.Add([]byte(`{"name":"test","value":42,"ok":true}`))
	f.Add([]byte(`{"name":"test","value":42,"ok":true,"extra":"field"}`))
	f.Add([]byte(`{"unknown_field":123}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"name":null,"value":"not_int"}`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		var target fuzzTarget
		//nolint:errcheck // fuzz: errors from arbitrary input are expected
		_, _ = jsonstrict.Unmarshal(data, &target)
	})
}
