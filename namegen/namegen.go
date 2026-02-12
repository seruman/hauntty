package namegen

import (
	"fmt"
	"math/rand/v2"
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

func Generate() string {
	adj := adjectives[rand.IntN(len(adjectives))]
	noun := nouns[rand.IntN(len(nouns))]
	return adj + "-" + noun
}

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

