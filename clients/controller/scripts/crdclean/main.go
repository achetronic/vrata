// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Command crdclean reads a CRD YAML file and strips all maxItems constraints
// and x-kubernetes-validations (CEL) entries from the OpenAPI schema, then
// writes the cleaned output to stdout or a file.
//
// Usage:
//
//	go run ./cmd/crdclean input.yaml > output.yaml
//	go run ./cmd/crdclean input.yaml output.yaml
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: crdclean <input.yaml> [output.yaml]\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "parsing YAML: %v\n", err)
		os.Exit(1)
	}

	removed := cleanNode(doc)
	fmt.Fprintf(os.Stderr, "crdclean: removed %d maxItems/CEL entries\n", removed)

	out, err := yaml.Marshal(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encoding YAML: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) >= 3 {
		if err := os.WriteFile(os.Args[2], out, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "writing %s: %v\n", os.Args[2], err)
			os.Exit(1)
		}
	} else {
		os.Stdout.Write(out)
	}
}

// cleanNode recursively removes maxItems and x-kubernetes-validations from a YAML tree.
func cleanNode(node any) int {
	removed := 0

	switch v := node.(type) {
	case map[string]any:
		if _, ok := v["maxItems"]; ok {
			delete(v, "maxItems")
			removed++
		}
		if _, ok := v["x-kubernetes-validations"]; ok {
			delete(v, "x-kubernetes-validations")
			removed++
		}
		for _, val := range v {
			removed += cleanNode(val)
		}
	case []any:
		for _, item := range v {
			removed += cleanNode(item)
		}
	}

	return removed
}

// Ensure json is imported (used implicitly by yaml for some edge cases).
var _ = json.Marshal
