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
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"text/template"
)

// FlagContext contains the variables available in flag templates
type FlagContext struct {
	InstanceID   string
	SourceID     string
	ChallengeID  string
	RandomString string
}

// DefaultTemplate is the default flag template if none is specified
// Note: Use standard Go template syntax {{.Variable}}
// Literal braces must be escaped or placed outside template actions
const DefaultTemplate = `FLAG{{"{"}}{{.ChallengeID}}_{{.SourceID}}_{{.RandomString}}{{"}"}}`

// Generate creates a unique flag based on the provided template and context
// Template syntax uses Go text/template with available fields:
// - .InstanceID: The instance name
// - .SourceID: The user/team identifier
// - .ChallengeID: The challenge identifier
// - .RandomString: A cryptographically secure random hex string (32 chars)
func Generate(tmpl string, instanceID, sourceID, challengeID string) (string, error) {
	if tmpl == "" {
		tmpl = DefaultTemplate
	}

	// Generate cryptographically secure random bytes
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	randomStr := hex.EncodeToString(randomBytes)

	// Create template context
	ctx := FlagContext{
		InstanceID:   instanceID,
		SourceID:     sourceID,
		ChallengeID:  challengeID,
		RandomString: randomStr,
	}

	// Parse and execute template
	t, err := template.New("flag").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse flag template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("failed to execute flag template: %w", err)
	}

	return buf.String(), nil
}

// GenerateMultiple generates multiple unique flags
func GenerateMultiple(tmpl string, instanceID, sourceID, challengeID string, count int) ([]string, error) {
	if count <= 0 {
		count = 1
	}

	flags := make([]string, count)
	for i := 0; i < count; i++ {
		flag, err := Generate(tmpl, instanceID, sourceID, challengeID)
		if err != nil {
			return nil, err
		}
		flags[i] = flag
	}

	return flags, nil
}
