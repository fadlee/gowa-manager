package config

import (
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestParseDefaults(t *testing.T) {
	cfg, action, err := Parse(nil, env(nil))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if action != ActionRun {
		t.Fatalf("action = %v", action)
	}
	if cfg.Port != 3000 || cfg.AdminUsername != "admin" || cfg.AdminPassword != "password" || cfg.DataDir != "./data" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseEnvironmentValues(t *testing.T) {
	cfg, _, err := Parse(nil, env(map[string]string{
		"PORT":           "4010",
		"ADMIN_USERNAME": "envuser",
		"ADMIN_PASSWORD": "envpass",
		"DATA_DIR":       "/env/data",
	}))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Port != 4010 || cfg.AdminUsername != "envuser" || cfg.AdminPassword != "envpass" || cfg.DataDir != "/env/data" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseCLIPrecedenceAndShortFlags(t *testing.T) {
	cfg, _, err := Parse([]string{"-p", "5000", "-u", "cliuser", "-P", "clipass", "-d", "/cli/data"}, env(map[string]string{
		"PORT":           "4010",
		"ADMIN_USERNAME": "envuser",
		"ADMIN_PASSWORD": "envpass",
		"DATA_DIR":       "/env/data",
	}))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Port != 5000 || cfg.AdminUsername != "cliuser" || cfg.AdminPassword != "clipass" || cfg.DataDir != "/cli/data" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseLongFlags(t *testing.T) {
	cfg, _, err := Parse([]string{"--port", "5001", "--admin-username", "user", "--admin-password", "pass", "--data-dir", "data2"}, env(nil))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Port != 5001 || cfg.AdminUsername != "user" || cfg.AdminPassword != "pass" || cfg.DataDir != "data2" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseHelpAndVersionActions(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want Action
	}{
		{[]string{"--help"}, ActionHelp},
		{[]string{"-h"}, ActionHelp},
		{[]string{"--version"}, ActionVersion},
		{[]string{"-v"}, ActionVersion},
		{[]string{"--port", "5000", "--help"}, ActionHelp},
	} {
		_, action, err := Parse(tc.args, env(nil))
		if err != nil {
			t.Fatalf("Parse(%v) error = %v", tc.args, err)
		}
		if action != tc.want {
			t.Fatalf("Parse(%v) action = %v, want %v", tc.args, action, tc.want)
		}
	}
}

func TestParseAllowsLeadingGoRunSeparator(t *testing.T) {
	_, action, err := Parse([]string{"--", "--help"}, env(nil))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if action != ActionHelp {
		t.Fatalf("action = %v, want %v", action, ActionHelp)
	}
}

func TestParseValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing port", []string{"--port"}, "❌ Missing value for --port"},
		{"empty username", []string{"--admin-username", ""}, "❌ Username cannot be empty"},
		{"long username", []string{"--admin-username", strings.Repeat("u", 51)}, "❌ Username cannot be longer than 50 characters"},
		{"empty password", []string{"--admin-password", ""}, "❌ Password cannot be empty"},
		{"long password", []string{"--admin-password", strings.Repeat("p", 101)}, "❌ Password cannot be longer than 100 characters"},
		{"low port", []string{"--port", "0"}, "❌ Invalid port: 0. Port must be between 1 and 65535."},
		{"high port", []string{"--port", "65536"}, "❌ Invalid port: 65536. Port must be between 1 and 65535."},
		{"unknown", []string{"--wat"}, "❌ Unknown option: --wat\nUse --help to see available options"},
		{"positional", []string{"extra"}, "❌ Unexpected argument: extra\nUse --help to see usage information"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Parse(tc.args, env(nil))
			if err == nil {
				t.Fatal("Parse() error = nil")
			}
			if err.Error() != tc.want {
				t.Fatalf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseRemovesDuplicatedExecutableArgument(t *testing.T) {
	for _, first := range []string{"gowa-manager", "gowa-manager-go.exe", "/tmp/gowa-manager-linux", `C:\tmp\gowa-manager-windows.exe`} {
		cfg, _, err := Parse([]string{first, "--port", "5050"}, env(nil))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", first, err)
		}
		if cfg.Port != 5050 {
			t.Fatalf("port = %d", cfg.Port)
		}
	}
}

func TestHelpAndVersionText(t *testing.T) {
	if !strings.Contains(HelpText(), "ADMIN_USERNAME") || !strings.Contains(HelpText(), "--admin-password") {
		t.Fatalf("HelpText() missing legacy options:\n%s", HelpText())
	}
	if !strings.Contains(VersionText(), "Built with Go") {
		t.Fatalf("VersionText() = %q", VersionText())
	}
}
