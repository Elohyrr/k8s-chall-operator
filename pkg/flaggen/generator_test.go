/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package flaggen

import (
	"strings"
	"testing"
)

func TestGenerate_DefaultTemplate(t *testing.T) {
	flag, err := Generate("", "instance-1", "user-123", "challenge-1")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if !strings.HasPrefix(flag, "FLAG{") {
		t.Errorf("Expected flag to start with 'FLAG{', got: %s", flag)
	}

	if !strings.HasSuffix(flag, "}") {
		t.Errorf("Expected flag to end with '}', got: %s", flag)
	}

	if !strings.Contains(flag, "challenge-1") {
		t.Errorf("Expected flag to contain challengeID, got: %s", flag)
	}

	if !strings.Contains(flag, "user-123") {
		t.Errorf("Expected flag to contain sourceID, got: %s", flag)
	}
}

func TestGenerate_CustomTemplate(t *testing.T) {
	// Use template escaping for literal braces: {{"{"}} outputs {
	tmpl := `CTF{{"{"}}{{.SourceID}}_{{.RandomString}}{{"}"}}`
	flag, err := Generate(tmpl, "instance-1", "team-42", "chall-5")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if !strings.HasPrefix(flag, "CTF{team-42_") {
		t.Errorf("Expected flag to start with 'CTF{team-42_', got: %s", flag)
	}
}

func TestGenerate_UniqueFlags(t *testing.T) {
	flags := make(map[string]bool)

	for i := 0; i < 100; i++ {
		flag, err := Generate("", "instance-1", "user-123", "challenge-1")
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}

		if flags[flag] {
			t.Errorf("Duplicate flag generated: %s", flag)
		}
		flags[flag] = true
	}
}

func TestGenerate_InvalidTemplate(t *testing.T) {
	// Invalid template syntax
	_, err := Generate("{{.Invalid", "instance-1", "user-123", "challenge-1")
	if err == nil {
		t.Error("Expected error for invalid template, got nil")
	}
}

func TestGenerate_AllVariables(t *testing.T) {
	tmpl := "{{.InstanceID}}-{{.SourceID}}-{{.ChallengeID}}-{{.RandomString}}"
	flag, err := Generate(tmpl, "inst-1", "src-2", "chall-3")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if !strings.HasPrefix(flag, "inst-1-src-2-chall-3-") {
		t.Errorf("Expected flag to contain all variables, got: %s", flag)
	}

	// Random string should be 32 hex chars
	parts := strings.Split(flag, "-")
	if len(parts) < 4 {
		t.Fatalf("Expected at least 4 parts, got: %d", len(parts))
	}

	randomPart := parts[len(parts)-1]
	if len(randomPart) != 32 {
		t.Errorf("Expected random string to be 32 chars, got: %d", len(randomPart))
	}
}

func TestGenerateMultiple(t *testing.T) {
	flags, err := GenerateMultiple("", "instance-1", "user-123", "challenge-1", 5)
	if err != nil {
		t.Fatalf("GenerateMultiple failed: %v", err)
	}

	if len(flags) != 5 {
		t.Errorf("Expected 5 flags, got: %d", len(flags))
	}

	// All flags should be unique
	seen := make(map[string]bool)
	for _, flag := range flags {
		if seen[flag] {
			t.Errorf("Duplicate flag in GenerateMultiple: %s", flag)
		}
		seen[flag] = true
	}
}

func TestGenerateMultiple_ZeroCount(t *testing.T) {
	flags, err := GenerateMultiple("", "instance-1", "user-123", "challenge-1", 0)
	if err != nil {
		t.Fatalf("GenerateMultiple failed: %v", err)
	}

	if len(flags) != 1 {
		t.Errorf("Expected 1 flag for count=0, got: %d", len(flags))
	}
}
