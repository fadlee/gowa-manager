package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fadlee/gowa-manager/internal/buildinfo"
)

type Config struct {
	Port int
	Host string
	// AdminUsername and AdminPassword seed the first superadmin when no
	// users exist yet. They are ignored once any user is registered.
	AdminUsername string
	AdminPassword string
	// AdminLinkSecret signs magic admin links. When empty, a random
	// secret persisted in the database is used.
	AdminLinkSecret    string
	DataDir            string
	SkipStartupInstall bool
	// MetricsEnabled controls the opt-in /metrics observability endpoint.
	// Disabled by default for safety; enable via GOWA_METRICS_ENABLED=1.
	MetricsEnabled bool
	// Verbose controls detailed (slog INFO-level) log output. When false
	// (default), only warnings and errors are logged to stderr and a
	// user-friendly startup banner is printed instead.
	Verbose bool
	// ResetUsername and ResetPassword are the arguments of the
	// reset-password command. An empty ResetPassword means "generate one".
	ResetUsername string
	ResetPassword string
}

type Action int

const (
	ActionRun Action = iota
	ActionHelp
	ActionVersion
	ActionResetPassword
)

func Parse(args []string, getenv func(string) string) (Config, Action, error) {
	cfg := Config{
		Port:               envPort(getenv("PORT")),
		Host:               envDefault(getenv("HOST"), "127.0.0.1"),
		AdminUsername:      getenv("ADMIN_USERNAME"),
		AdminPassword:      getenv("ADMIN_PASSWORD"),
		AdminLinkSecret:    getenv("ADMIN_LINK_SECRET"),
		DataDir:            envDefault(getenv("DATA_DIR"), "./data"),
		SkipStartupInstall: envBool(getenv("GOWA_SKIP_STARTUP_INSTALL")),
		MetricsEnabled:     envBool(getenv("GOWA_METRICS_ENABLED")),
		Verbose:            envBool(getenv("GOWA_VERBOSE")),
	}
	args = stripDuplicatedExecutable(args)
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	action := ActionRun
	if len(args) > 0 && args[0] == "reset-password" {
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return cfg, action, errors.New("❌ Usage: gowa-manager reset-password <username> [--password <pass>] [--data-dir <path>]")
		}
		action = ActionResetPassword
		cfg.ResetUsername = args[1]
		args = args[2:]
	}
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
		case "--verbose":
			cfg.Verbose = true
		case "--password":
			if action != ActionResetPassword {
				return cfg, action, fmt.Errorf("❌ Unknown option: %s\nUse --help to see available options", arg)
			}
			value, next, err := valueArg(args, i, arg)
			if err != nil {
				return cfg, action, err
			}
			cfg.ResetPassword = value
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
  gowa-manager reset-password <username> [--password <pass>] [-d <path>]

OPTIONS:
  -p, --port <port>              Server port (default: 3000)
  --host <host>                  Server bind host (default: 127.0.0.1; use 0.0.0.0 for Docker)
  -u, --admin-username <user>    Initial superadmin username (only used while no users exist)
  -P, --admin-password <pass>    Initial superadmin password (only used while no users exist)
  -d, --data-dir <path>          Data directory (default: ./data)
      --verbose                  Detailed log output (default: off)
  -h, --help                     Show this help message
  -v, --version                  Show version information

EXAMPLES:
  gowa-manager                                    # Run with defaults
  gowa-manager --port 8080                       # Custom port
  gowa-manager -u admin -P mypassword            # Seed the first superadmin
  gowa-manager reset-password admin              # Reset a forgotten password

When no users exist and no initial superadmin is configured, open the Web UI
and register the first account (it becomes superadmin) using the setup token
printed at startup.

ENVIRONMENT VARIABLES:
  PORT              Server port
  HOST              Server bind host (default: 127.0.0.1; use 0.0.0.0 for Docker)
  ADMIN_USERNAME    Initial superadmin username (only used while no users exist)
  ADMIN_PASSWORD    Initial superadmin password (only used while no users exist)
  ADMIN_LINK_SECRET Secret for magic admin links (default: random, stored in DB)
  DATA_DIR          Data directory
  GOWA_METRICS_ENABLED  Enable loopback-only /metrics endpoint (0/1)
  GOWA_SKIP_STARTUP_INSTALL  Skip automatic latest GOWA install on startup (0/1)
  GOWA_VERBOSE               Detailed log output, same as --verbose (0/1)
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
