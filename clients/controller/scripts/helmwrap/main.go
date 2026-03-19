// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Command helmwrap reads a raw CRD YAML file and wraps it with Helm
// template guards so it is only installed when the controller is enabled
// and superHttpRoute is true. It also injects the standard Helm labels.
//
// Usage:
//
//	go run ./scripts/helmwrap input.yaml output.yaml
package main

import (
	"fmt"
	"os"
	"strings"
)

const header = `{{- if .Values.controller.enabled }}
{{- if .Values.controller.installCRDs }}
`

const labelsBlock = `    labels:
        {{- include "vrata.labels" . | nindent 8 }}
`

const footer = `{{- end }}
{{- end }}
`

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: helmwrap <input.yaml> <output.yaml>\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}

	content := string(data)

	// Inject Helm labels after metadata: / name: line.
	lines := strings.Split(content, "\n")
	var result []string
	injected := false
	for _, line := range lines {
		result = append(result, line)
		if !injected && strings.TrimSpace(line) == "name: superhttproutes.vrata.io" {
			result = append(result, labelsBlock)
			injected = true
		}
	}

	wrapped := header + strings.Join(result, "\n") + "\n" + footer

	if err := os.WriteFile(os.Args[2], []byte(wrapped), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", os.Args[2], err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "helmwrap: wrote %s\n", os.Args[2])
}
