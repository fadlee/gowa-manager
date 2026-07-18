package instances

import (
	"crypto/rand"
	"math"
	mathrand "math/rand"
)

var adjectives = []string{"fast", "swift", "bright", "cool", "smart", "quick", "active", "dynamic"}
var nouns = []string{"app", "service", "worker", "server", "instance", "process", "handler", "engine"}

func RandomName(random func() float64) string {
	if random == nil {
		random = mathrand.Float64
	}
	adjective := adjectives[randomIndex(random(), len(adjectives))]
	noun := nouns[randomIndex(random(), len(nouns))]
	number := randomIndex(random(), 1000)
	return adjective + "-" + noun + "-" + itoa(number)
}

func GenerateKey() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	key := make([]byte, 8)
	for i, value := range bytes {
		key[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(key), nil
}

func randomIndex(value float64, length int) int {
	index := int(math.Floor(value * float64(length)))
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
