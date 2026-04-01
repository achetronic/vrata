// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package resolve handles {{secret:<source>:<ref>}} pattern resolution in
// serialized JSON snapshots. All resolution happens on the control plane
// at snapshot build time. The proxy never sees unresolved patterns.
package resolve

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/achetronic/vrata/internal/store"
)

// secretPattern matches {{secret:<source>:<ref>}} where source is one of
// value, env, or file, and ref is the secret ID, env var name, or file path.
var secretPattern = regexp.MustCompile(`\{\{secret:(value|env|file):([^}]+)\}\}`)

// Secrets scans the JSON data for {{secret:...}} patterns and resolves them.
// Returns the resolved JSON or an error listing all unresolved references.
//
// Sources:
//   - value:<id>  — looks up the Secret by ID in the store
//   - env:<name>  — reads os.Getenv(name) on the control plane
//   - file:<path> — reads os.ReadFile(path) on the control plane
func Secrets(ctx context.Context, st store.Store, data []byte) ([]byte, error) {
	var errs []string

	resolved := secretPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		parts := secretPattern.FindSubmatch(match)
		if len(parts) != 3 {
			errs = append(errs, fmt.Sprintf("malformed pattern: %s", match))
			return match
		}

		source := string(parts[1])
		ref := string(parts[2])

		var value string
		var err error

		switch source {
		case "value":
			sec, lookupErr := st.GetSecret(ctx, ref)
			if lookupErr != nil {
				errs = append(errs, fmt.Sprintf("secret %q: %v", ref, lookupErr))
				return match
			}
			value = sec.Value
		case "env":
			value = os.Getenv(ref)
			if value == "" {
				errs = append(errs, fmt.Sprintf("env var %q is not set", ref))
				return match
			}
		case "file":
			content, readErr := os.ReadFile(ref)
			if readErr != nil {
				errs = append(errs, fmt.Sprintf("file %q: %v", ref, readErr))
				return match
			}
			value = string(content)
		default:
			errs = append(errs, fmt.Sprintf("unknown source %q in %s", source, match))
			return match
		}

		_ = err
		return []byte(escapeJSON(value))
	})

	if len(errs) > 0 {
		return nil, fmt.Errorf("unresolved secret references:\n  %s", strings.Join(errs, "\n  "))
	}

	return resolved, nil
}

// escapeJSON escapes a string value so it is safe inside a JSON string literal.
// The replacement happens inside an already-quoted JSON value, so we need to
// escape backslashes, quotes, and control characters.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
