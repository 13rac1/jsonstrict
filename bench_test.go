package jsonstrict_test

import (
	"encoding/json"
	"testing"

	"github.com/13rac1/jsonstrict"
)

// benchFlat matches benchFlatJSON; benchNested matches benchNestedJSON.
type benchFlat struct {
	Name    string  `json:"name"`
	Value   int     `json:"value"`
	OK      bool    `json:"ok"`
	Ratio   float64 `json:"ratio"`
	Comment string  `json:"comment,omitempty"`
}

type benchNested struct {
	ID       string                   `json:"id"`
	Shipping nestedAddress            `json:"shipping"`
	Billing  *nestedAddress           `json:"billing"`
	Items    []nestedAddress          `json:"items"`
	Configs  map[string]nestedAddress `json:"configs"`
}

var (
	benchFlatJSON = []byte(`{"name":"alice","value":42,"ok":true,"ratio":0.75,"comment":"hi","extra":"x"}`)

	benchNestedJSON = []byte(`{
		"id": "o-1",
		"shipping": {"street": "1 Main St", "zip": "90210"},
		"billing": {"street": "2 Oak Ave", "zipp": "10001"},
		"items": [
			{"street": "a", "zip": "1"},
			{"street": "b"},
			{"street": "c", "zip": "3", "legacy": true}
		],
		"configs": {"dev": {"street": "d", "zip": "4"}, "prod": {"street": "p"}}
	}`)
)

func BenchmarkUnmarshalFlat(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		var v benchFlat
		if _, err := jsonstrict.Unmarshal(benchFlatJSON, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStdlibUnmarshalFlat(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		var v benchFlat
		if err := json.Unmarshal(benchFlatJSON, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalNested(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		var v benchNested
		if _, err := jsonstrict.Unmarshal(benchNestedJSON, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStdlibUnmarshalNested(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		var v benchNested
		if err := json.Unmarshal(benchNestedJSON, &v); err != nil {
			b.Fatal(err)
		}
	}
}
