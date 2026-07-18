package buildinfo

import "testing"

func TestDisplayVersion(t *testing.T) {
	old := Version
	Version = "1.8.1-test"
	t.Cleanup(func() { Version = old })
	if got := DisplayVersion(); got != "GOWA Manager v1.8.1-test" {
		t.Fatalf("DisplayVersion() = %q", got)
	}
}
