package namegen

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

var adjectives = []string{
	"phantom", "hollow", "silver", "shadow", "spectral",
	"ghostly", "ethereal", "haunted", "mystic", "twilight",
	"silent", "fading", "ancient", "cursed", "forgotten",
	"pale", "dark", "eerie", "somber", "shrouded",
	"veiled", "grim", "dusk", "frost", "ashen",
	"waning", "void", "deep", "lost", "still",
}

var nouns = []string{
	"drift", "echo", "mist", "shade", "whisper",
	"wraith", "specter", "haunt", "gloom", "crypt",
	"tomb", "veil", "fog", "dusk", "ember",
	"ash", "bone", "rune", "ward", "gate",
	"marsh", "moor", "vale", "rift", "cairn",
	"peak", "keep", "den", "maze", "well",
}

// Generate returns a random ghost-themed name in "adjective-noun" format.
func Generate() string {
	adj := adjectives[rand.IntN(len(adjectives))]
	noun := nouns[rand.IntN(len(nouns))]
	return adj + "-" + noun
}

// GenerateUnique returns a name not in the provided set of existing names.
// Retries up to 100 times, then appends a random digit suffix.
func GenerateUnique(existing map[string]bool) string {
	for range 100 {
		name := Generate()
		if !existing[name] {
			return name
		}
	}
	// Exhausted retries; append a random suffix.
	name := Generate()
	return fmt.Sprintf("%s-%d", name, rand.IntN(1000))
}

// MaxCombinations returns the total number of unique adjective-noun pairs.
func MaxCombinations() int {
	return len(adjectives) * len(nouns)
}

// IsValid checks if a name matches the "adjective-noun" format from the word lists.
func IsValid(name string) bool {
	parts := strings.SplitN(name, "-", 2)
	if len(parts) != 2 {
		return false
	}
	adjOK := false
	for _, a := range adjectives {
		if a == parts[0] {
			adjOK = true
			break
		}
	}
	if !adjOK {
		return false
	}
	for _, n := range nouns {
		if n == parts[1] {
			return true
		}
	}
	return false
}
