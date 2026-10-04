package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const validScroll = "name: fixture\ndesc: Fixture\nversion: 1.0.0\napp_version: '1'\ncommands:\n  start:\n    procedures:\n      - image: busybox:1.36\n        command: [sleep, '600']\n"

func TestValidateUsesRuntimeSemanticsWithoutHostExpansion(t *testing.T) {
	t.Setenv("SECRET_IMAGE", "")
	for _, fixture := range []struct {
		name, yaml string
		valid      bool
	}{
		{"valid", validScroll, true},
		{"literal environment placeholder", strings.ReplaceAll(validScroll, "busybox:1.36", "$SECRET_IMAGE"), true},
		{"missing description", strings.ReplaceAll(validScroll, "desc: Fixture\n", ""), false},
		{"invalid version", strings.ReplaceAll(validScroll, "version: 1.0.0", "version: invalid"), false},
		{"empty procedures", "name: fixture\ndesc: Fixture\nversion: 1.0.0\napp_version: '1'\ncommands:\n  start: {}\n", false},
		{"missing image", strings.ReplaceAll(validScroll, "image: busybox:1.36", "image: ''"), false},
		{"signal without target", strings.ReplaceAll(validScroll, "image: busybox:1.36", "type: signal\n        signal: SIGTERM"), false},
		{"escaping mount", validScroll + "        mounts:\n          - path: /server\n            sub_path: ../secret\n", false},
		{"relative mount", validScroll + "        mounts:\n          - path: server\n", false},
		{"unknown expected port", validScroll + "        expectedPorts:\n          - name: missing\n", false},
		{"duplicate ids", validScroll + "        id: duplicate\n      - image: busybox\n        id: duplicate\n", false},
		{"legacy mode", validScroll + "        mode: exec\n", false},
		{"malformed yaml", "commands: [", false},
		{"oversized input", strings.Repeat("x", maxInputBytes+1), false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var output bytes.Buffer
			validate(strings.NewReader(fixture.yaml), &output)
			var result struct {
				Version int  `json:"version"`
				Valid   bool `json:"valid"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Version != 1 || result.Valid != fixture.valid {
				t.Fatalf("got %+v, want valid=%v", result, fixture.valid)
			}
		})
	}
}
