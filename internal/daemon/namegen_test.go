package daemon

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestGenerateName(t *testing.T) {
	for range 100 {
		name := generateName()
		parts := strings.SplitN(name, "-", 2)
		assert.Equal(t, len(parts), 2, "expected adj-noun format, got %q", name)
		assert.Assert(t, parts[0] != "", "empty adjective in %q", name)
		assert.Assert(t, parts[1] != "", "empty noun in %q", name)
	}
}

func TestGenerateUniqueName_AvoidsCollisions(t *testing.T) {
	existing := map[string]bool{}
	for range 50 {
		name := generateUniqueName(existing)
		assert.Assert(t, !existing[name], "generateUniqueName returned duplicate %q", name)
		existing[name] = true
	}
}

func TestGenerateUniqueName_ExhaustedSpace(t *testing.T) {
	existing := make(map[string]bool, len(adjectives)*len(nouns))
	for _, adj := range adjectives {
		for _, noun := range nouns {
			existing[adj+"-"+noun] = true
		}
	}

	name := generateUniqueName(existing)
	assert.Assert(t, name != "", "generateUniqueName returned empty string")
	parts := strings.Split(name, "-")
	assert.Assert(t, len(parts) >= 3, "expected suffixed name, got %q", name)
}
