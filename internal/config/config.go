package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fadlee/gowa-manager/internal/buildinfo"
)

type Config struct {
	Port          int
	Host          string
	AdminUsername string
	AdminPassword string
	DataDir       string
	// MetricsEnabled controls the opt-in /metrics observability endpoint.
	// Disabled by default for safety; enable via GOWA_METRICS_ENABLED=1.
	MetricsEnabled bool
}

type Action int

const (
	ActionRun Action = iota
	ActionHelp
	ActionVersion
)

func Parse(args []string, getenv func(string) string) (Config, Action, error) {
	cfg := Config{
		Port:           envPort(getenv("PORT")),
		Host:           envDefault(getenv("HOST"), "127.0.0.1"),
		AdminUsername:  envDefault(getenv("ADMIN_USERNAME"), "admin"),
		AdminPassword:  envDefault(getenv("ADMIN_PASSWORD"), "password"),
		DataDir:        envDefault(getenv("DATA_DIR"), "./data"),
		MetricsEnabled: envBool(getenv("GOWA_METRICS_ENABLED")),
	}
	args = stripDuplicatedExecutable(args)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	action := ActionRun
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			action = ActionHelp
		case "-v", "--version":
			action = ActionVersion
		case "-p", "--port":
			value, next, err := valueArg(args, i, arg)
			if err != nil {
				return cfg, action, err
			}
			port, err := parsePort(value)
			if err != nil {
				return cfg, action, err
			}
			cfg.Port = port
			i = next
		case "--host":
			value, next, err := valueArg(args, i, arg)
			if err != nil {
				return cfg, action, err
			}
			cfg.Host = value
			i = next
		case "-u", "--admin-username":
			value, next, err := valueArg(args, i, arg)
			if err != nil {
				return cfg, action, err
			}
			if err := validateUsername(value); err != nil {
				return cfg, action, err
			}
			cfg.AdminUsername = value
			i = next
		case "-P", "--admin-password":
			value, next, err := valueArg(args, i, arg)
			if err != nil {
				return cfg, action, err
			}
			if err := validatePassword(value); err != nil {
				return cfg, action, err
			}
			cfg.AdminPassword = value
			i = next
		case "-d", "--data-dir":
			value, next, err := valueArg(args, i, arg)
			if err != nil {
				return cfg, action, err
			}
			cfg.DataDir = value
			i = next
		default:
			if strings.HasPrefix(arg, "-") {
				return cfg, action, fmt.Errorf("❌ Unknown option: %s\nUse --help to see available options", arg)
			}
			return cfg, action, fmt.Errorf("❌ Unexpected argument: %s\nUse --help to see usage information", arg)
		}
	}
	return cfg, action, nil
}

func HelpText() string {
	return `
🚀 GOWA Manager - WhatsApp Instance Manager

USAGE:
  gowa-manager [OPTIONS]

OPTIONS:
  -p, --port <port>              Server port (default: 3000)
  --host <host>                  Server bind host (default: 127.0.0.1; use 0.0.0.0 for Docker)
  -u, --admin-username <user>    Admin username (default: admin)
  -P, --admin-password <pass>    Admin password (default: password)
  -d, --data-dir <path>          Data directory (default: ./data)
  -h, --help                     Show this help message
  -v, --version                  Show version information

EXAMPLES:
  gowa-manager                                    # Run with defaults
  gowa-manager --port 8080                       # Custom port
  gowa-manager -u admin -P mypassword            # Custom credentials
  gowa-manager --port 8080 -u myuser -P mypass   # Full custom config

ENVIRONMENT VARIABLES:
  PORT              Server port
  HOST              Server bind host (default: 127.0.0.1; use 0.0.0.0 for Docker)
  ADMIN_USERNAME    Admin username
  ADMIN_PASSWORD    Admin password
  DATA_DIR          Data directory
  GOWA_METRICS_ENABLED  Enable loopback-only /metrics endpoint (0/1)

Note: Command line arguments take precedence over environment variables.

For more information, visit: https://github.com/fadlee/gowa-manager
`
}

func VersionText() string {
	return buildinfo.DisplayVersion() + "\nBuilt with Go"
}

func envDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func envPort(value string) int {
	if value == "" {
		return 3000
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 3000
	}
	return port
}

// envBool interprets a truthy env value as true. Accepts 1, t, T, TRUE,
// true, True (matching strconv.ParseBool); anything else is false.
func envBool(value string) bool {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return b
}

func valueArg(args []string, index int, flag string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("❌ Missing value for %s", flag)
	}
	return args[index+1], index + 1, nil
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("❌ Invalid port: %s. Port must be between 1 and 65535.", value)
	}
	return port, nil
}

func validateUsername(username string) error {
	if len(username) < 1 {
		return errors.New("❌ Username cannot be empty")
	}
	if len(username) > 50 {
		return errors.New("❌ Username cannot be longer than 50 characters")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 1 {
		return errors.New("❌ Password cannot be empty")
	}
	if len(password) > 100 {
		return errors.New("❌ Password cannot be longer than 100 characters")
	}
	return nil
}

func stripDuplicatedExecutable(args []string) []string {
	if len(args) == 0 {
		return args
	}
	first := args[0]
	if strings.HasSuffix(first, "gowa-manager") || strings.HasSuffix(first, ".exe") || strings.Contains(first, "/gowa-manager-") || strings.Contains(first, `\gowa-manager-`) {
		return args[1:]
	}
	return args
}
