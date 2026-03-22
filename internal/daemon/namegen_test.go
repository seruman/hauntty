package daemon

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestGenerateName(t *testing.T) {
	oldAdjectives := adjectives
	oldNouns := nouns
	adjectives = []string{"alpha"}
	nouns = []string{"beta"}
	defer func() {
		adjectives = oldAdjectives
		nouns = oldNouns
	}()

	got := generateName()
	assert.Equal(t, got, "alpha-beta")
}

func TestGenerateUniqueNameReturnsOnlyAvailableCombination(t *testing.T) {
	oldAdjectives := adjectives
	oldNouns := nouns
	adjectives = []string{"alpha"}
	nouns = []string{"beta", "gamma"}
	defer func() {
		adjectives = oldAdjectives
		nouns = oldNouns
	}()

	got := generateUniqueName(map[string]bool{"alpha-beta": true})
	assert.Equal(t, got, "alpha-gamma")
}

func TestGenerateUniqueNameAddsNumericSuffixWhenExhausted(t *testing.T) {
	oldAdjectives := adjectives
	oldNouns := nouns
	adjectives = []string{"alpha"}
	nouns = []string{"beta"}
	defer func() {
		adjectives = oldAdjectives
		nouns = oldNouns
	}()

	got := generateUniqueName(map[string]bool{
		"alpha-beta":   true,
		"alpha-beta-0": true,
	})
	assert.Equal(t, got, "alpha-beta-1")
}
