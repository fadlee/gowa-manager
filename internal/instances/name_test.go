package instances

import (
	"regexp"
	"testing"
)

func TestNameRandomNameGeneratesExpectedFormat(t *testing.T) {
	name := RandomName(nil)
	pattern := regexp.MustCompile(`^(fast|swift|bright|cool|smart|quick|active|dynamic)-(app|service|worker|server|instance|process|handler|engine)-\d{1,3}$`)
	if !pattern.MatchString(name) {
		t.Fatalf("RandomName() = %q, want adjective-noun-number", name)
	}
}

func TestNameRandomNameUsesRandomValues(t *testing.T) {
	values := []float64{0, 0.999, 0.123}
	index := 0
	name := RandomName(func() float64 {
		value := values[index]
		index++
		return value
	})

	if name != "fast-engine-123" {
		t.Fatalf("RandomName deterministic = %q, want fast-engine-123", name)
	}
	if index != 3 {
		t.Fatalf("random called %d times, want 3", index)
	}
}

func TestNameRandomNameSupportsUpperBoundValues(t *testing.T) {
	name := RandomName(func() float64 { return 0.999 })
	if name != "dynamic-engine-999" {
		t.Fatalf("RandomName upper bound = %q, want dynamic-engine-999", name)
	}
}

func TestNameGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	if !regexp.MustCompile(`^[A-Z0-9]{8}$`).MatchString(key) {
		t.Fatalf("GenerateKey() = %q, want 8 uppercase alphanumeric chars", key)
	}
}

func FuzzGenerateKeyFormat(f *testing.F) {
	f.Add(1)
	f.Fuzz(func(t *testing.T, _ int) {
		key, err := GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey() error = %v", err)
		}
		if !regexp.MustCompile(`^[A-Z0-9]{8}$`).MatchString(key) {
			t.Fatalf("GenerateKey() = %q, want 8 uppercase alphanumeric chars", key)
		}
	})
}
