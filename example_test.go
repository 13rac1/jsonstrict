package jsonstrict_test

import (
	"fmt"
	"sort"

	"github.com/13rac1/jsonstrict"
)

type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func ExampleUnmarshal() {
	data := []byte(`{"name":"alice","email":"alice@example.com"}`)

	var user User
	result, err := jsonstrict.Unmarshal(data, &user)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("name:", user.Name)
	fmt.Println("unknown:", len(result.Unknown))
	fmt.Println("missing:", len(result.Missing))
	// Output:
	// name: alice
	// unknown: 0
	// missing: 0
}

func ExampleUnmarshal_unknownFields() {
	data := []byte(`{"name":"alice","email":"alice@example.com","role":"admin","age":30}`)

	var user User
	result, err := jsonstrict.Unmarshal(data, &user)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	// Unknown fields are sorted by key for stable output.
	keys := make([]string, 0, len(result.Unknown))
	for k := range result.Unknown {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("unknown %s: %s\n", k, result.Unknown[k])
	}
	// Output:
	// unknown age: 30
	// unknown role: "admin"
}

func ExampleUnmarshal_missingFields() {
	data := []byte(`{"name":"alice"}`)

	var user User
	result, err := jsonstrict.Unmarshal(data, &user)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("missing:", result.Missing)
	// Output:
	// missing: [email]
}
