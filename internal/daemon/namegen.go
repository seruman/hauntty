package daemon

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

func generateName() string {
	adj := adjectives[rand.IntN(len(adjectives))]
	noun := nouns[rand.IntN(len(nouns))]
	return adj + "-" + noun
}

func generateUniqueName(existing map[string]bool) string {
	for range 100 {
		name := generateName()
		if !existing[name] {
			return name
		}
	}
	name := generateName()
	return fmt.Sprintf("%s-%d", name, rand.IntN(1000))
}
