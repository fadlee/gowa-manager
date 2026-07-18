package instances

import (
	"crypto/rand"
	"math"
	"math/big"
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
	key := make([]byte, 8)
	max := big.NewInt(int64(len(alphabet)))
	for i := range key {
		index, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		key[i] = alphabet[index.Int64()]
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
