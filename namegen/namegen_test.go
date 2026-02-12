package namegen

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestGenerate(t *testing.T) {
	for range 100 {
		name := Generate()
		parts := strings.SplitN(name, "-", 2)
		assert.Equal(t, len(parts), 2, "expected adj-noun format, got %q", name)
		assert.Assert(t, parts[0] != "", "empty adjective in %q", name)
		assert.Assert(t, parts[1] != "", "empty noun in %q", name)
		assert.Assert(t, IsValid(name), "generated name %q is not valid", name)
	}
}

func TestGenerateUnique_AvoidsCollisions(t *testing.T) {
	existing := map[string]bool{}
	for range 50 {
		name := GenerateUnique(existing)
		assert.Assert(t, !existing[name], "GenerateUnique returned duplicate %q", name)
		existing[name] = true
	}
}

func TestGenerateUnique_ExhaustedSpace(t *testing.T) {
	existing := make(map[string]bool, MaxCombinations())
	for _, adj := range adjectives {
		for _, noun := range nouns {
			existing[adj+"-"+noun] = true
		}
	}

	name := GenerateUnique(existing)
	assert.Assert(t, name != "", "GenerateUnique returned empty string")
	parts := strings.Split(name, "-")
	assert.Assert(t, len(parts) >= 3, "expected suffixed name, got %q", name)
}
