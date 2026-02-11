package namegen

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	for range 100 {
		name := Generate()
		parts := strings.SplitN(name, "-", 2)
		if len(parts) != 2 {
			t.Fatalf("expected adj-noun format, got %q", name)
		}
		if parts[0] == "" || parts[1] == "" {
			t.Fatalf("empty part in %q", name)
		}
		if !IsValid(name) {
			t.Fatalf("generated name %q is not valid", name)
		}
	}
}

func TestGenerateUnique_AvoidsCollisions(t *testing.T) {
	existing := map[string]bool{}
	for range 50 {
		name := GenerateUnique(existing)
		if existing[name] {
			t.Fatalf("GenerateUnique returned duplicate %q", name)
		}
		existing[name] = true
	}
}

func TestGenerateUnique_ExhaustedSpace(t *testing.T) {
	// Fill all combinations into existing set.
	existing := make(map[string]bool, MaxCombinations())
	for _, adj := range adjectives {
		for _, noun := range nouns {
			existing[adj+"-"+noun] = true
		}
	}

	// Should still return a name with a suffix.
	name := GenerateUnique(existing)
	if name == "" {
		t.Fatal("GenerateUnique returned empty string")
	}
	// The name should have a digit suffix (3 dashes: adj-noun-N).
	parts := strings.Split(name, "-")
	if len(parts) < 3 {
		t.Fatalf("expected suffixed name, got %q", name)
	}
}
