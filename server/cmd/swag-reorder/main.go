// swag-reorder rewrites a swagger.json produced by swag v2 so that the
// "openapi" and "info" keys appear first. swag v2.0.0 emits them after
// "components", which causes Swagger UI to reject the spec with
// "does not specify a valid version field".
//
// Usage:
//
//	swag-reorder <path/to/swagger.json>
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: swag-reorder <swagger.json>")
		os.Exit(1)
	}

	path := os.Args[1]

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "swag-reorder: read %s: %v\n", path, err)
		os.Exit(1)
	}

	// Decode into a generic ordered map using json.RawMessage so we preserve
	// every nested value exactly as-is.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "swag-reorder: parse %s: %v\n", path, err)
		os.Exit(1)
	}

	// Build output with openapi + info first, then everything else.
	priority := []string{"openapi", "info"}
	var buf bytes.Buffer
	buf.WriteString("{\n")

	written := 0
	total := len(raw)

	writeKey := func(k string, v json.RawMessage) {
		if written > 0 {
			buf.WriteString(",\n")
		}
		keyBytes, _ := json.Marshal(k)
		buf.Write(keyBytes)
		buf.WriteString(": ")
		buf.Write(v)
		written++
	}

	for _, k := range priority {
		if v, ok := raw[k]; ok {
			writeKey(k, v)
		}
	}

	prioritySet := make(map[string]bool, len(priority))
	for _, k := range priority {
		prioritySet[k] = true
	}

	// Remaining keys — iterate the original JSON token stream to preserve
	// insertion order (Go maps are unordered, but for non-priority keys order
	// doesn't matter for correctness, only for readability).
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.Token() // opening {
	for dec.More() {
		t, _ := dec.Token()
		k := t.(string)
		var v json.RawMessage
		dec.Decode(&v) //nolint:errcheck
		if !prioritySet[k] {
			writeKey(k, v)
		}
	}

	_ = total
	buf.WriteString("\n}\n")

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "swag-reorder: write %s: %v\n", path, err)
		os.Exit(1)
	}
}
