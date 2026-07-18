package instances

import (
	"encoding/json"
	"strconv"
	"strings"
)

type BasicAuthPair struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CLIFlags struct {
	AccountValidation *bool           `json:"accountValidation,omitempty"`
	BasicAuth         []BasicAuthPair `json:"basicAuth,omitempty"`
	OS                string          `json:"os,omitempty"`
	Webhooks          []string        `json:"webhooks,omitempty"`
	DisabledWebhooks  []string        `json:"disabledWebhooks,omitempty"`
	AutoMarkRead      *bool           `json:"autoMarkRead,omitempty"`
	AutoReply         string          `json:"autoReply,omitempty"`
	BasePath          string          `json:"basePath,omitempty"`
	Debug             *bool           `json:"debug,omitempty"`
	WebhookSecret     string          `json:"webhookSecret,omitempty"`
	Raw               map[string]json.RawMessage
}

type InstanceConfig struct {
	Args    any `json:"args,omitempty"`
	Env     any `json:"env,omitempty"`
	EnvVars any `json:"envVars,omitempty"`
	Flags   CLIFlags
	Raw     map[string]json.RawMessage
}

func ParseConfig(configString string) InstanceConfig {
	config, ok := parseConfigObject(configString)
	if !ok {
		return InstanceConfig{Raw: map[string]json.RawMessage{}}
	}
	return config
}

func DefaultConfig() InstanceConfig {
	accountValidation := true
	config := InstanceConfig{
		Args: []string{"rest", "--port=PORT"},
		Flags: CLIFlags{
			AccountValidation: &accountValidation,
			OS:                "GowaManager",
		},
		Raw: map[string]json.RawMessage{},
	}
	config.Raw["args"] = mustMarshal(config.Args)
	config.Raw["flags"] = flagsToRaw(config.Flags)
	return config
}

func FlagsToArgs(flags CLIFlags) []string {
	args := []string{}
	if flags.AccountValidation != nil {
		args = append(args, "--account-validation="+strconv.FormatBool(*flags.AccountValidation))
	}
	for _, auth := range flags.BasicAuth {
		args = append(args, "--basic-auth="+auth.Username+":"+auth.Password)
	}
	if flags.OS != "" {
		args = append(args, "--os="+flags.OS)
	}
	if len(flags.Webhooks) > 0 {
		disabled := map[string]bool{}
		for _, webhook := range flags.DisabledWebhooks {
			disabled[webhook] = true
		}
		for _, webhook := range flags.Webhooks {
			if !disabled[webhook] {
				args = append(args, "--webhook="+webhook)
			}
		}
	}
	if flags.AutoMarkRead != nil {
		args = append(args, "--auto-mark-read="+strconv.FormatBool(*flags.AutoMarkRead))
	}
	if flags.AutoReply != "" {
		args = append(args, "--autoreply="+flags.AutoReply)
	}
	if flags.BasePath != "" {
		args = append(args, "--base-path="+flags.BasePath)
	}
	if flags.Debug != nil {
		args = append(args, "--debug="+strconv.FormatBool(*flags.Debug))
	}
	if flags.WebhookSecret != "" {
		args = append(args, "--webhook-secret="+flags.WebhookSecret)
	}
	return args
}

func ProcessArgs(config InstanceConfig, port int) []string {
	args := []string{}
	switch value := config.Args.(type) {
	case []string:
		args = append(args, value...)
	case string:
		if strings.TrimSpace(value) != "" {
			args = append(args, strings.Fields(value)...)
		}
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok {
				args = append(args, text)
			}
		}
	}
	args = append(args, FlagsToArgs(config.Flags)...)
	portText := strconv.Itoa(port)
	for i, arg := range args {
		args[i] = strings.ReplaceAll(arg, "PORT", portText)
	}
	return args
}

func ParseEnvironmentVars(config InstanceConfig, port int, base map[string]string) map[string]string {
	env := map[string]string{}
	for key, value := range base {
		env[key] = value
	}
	env["PORT"] = strconv.Itoa(port)

	if config.Env != nil {
		mergeEnvValue(env, config.Env)
	} else if config.EnvVars != nil {
		mergeEnvValue(env, config.EnvVars)
	}
	return env
}

func NormalizeUpdateConfig(existingConfig string, nextConfig *string, instanceKey string) string {
	source := existingConfig
	if nextConfig != nil {
		source = *nextConfig
	}
	config, ok := parseConfigObject(source)
	if !ok && nextConfig != nil {
		config, ok = parseConfigObject(existingConfig)
	}
	if !ok {
		config = InstanceConfig{Raw: map[string]json.RawMessage{}}
	}
	config.Flags.BasePath = "/app/" + instanceKey
	return configToJSON(config)
}

func BuildCreateConfig(userConfig *string, instanceKey string) string {
	config := DefaultConfig()
	if userConfig != nil {
		if parsed, ok := parseConfigObject(*userConfig); ok {
			config = mergeCreateConfig(config, parsed)
		}
	}
	config.Flags.BasePath = "/app/" + instanceKey
	return configToJSON(config)
}

func parseConfigObject(configString string) (InstanceConfig, bool) {
	if strings.TrimSpace(configString) == "" {
		return InstanceConfig{Raw: map[string]json.RawMessage{}}, true
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(configString), &raw); err != nil || raw == nil {
		return InstanceConfig{}, false
	}
	return configFromRaw(raw), true
}

func configFromRaw(raw map[string]json.RawMessage) InstanceConfig {
	config := InstanceConfig{Raw: cloneRaw(raw)}
	if value, ok := raw["args"]; ok {
		_ = json.Unmarshal(value, &config.Args)
	}
	if value, ok := raw["env"]; ok {
		_ = json.Unmarshal(value, &config.Env)
	}
	if value, ok := raw["envVars"]; ok {
		_ = json.Unmarshal(value, &config.EnvVars)
	}
	if value, ok := raw["flags"]; ok {
		var flagsRaw map[string]json.RawMessage
		if err := json.Unmarshal(value, &flagsRaw); err == nil && flagsRaw != nil {
			config.Flags = flagsFromRaw(flagsRaw)
		}
	}
	return config
}

func flagsFromRaw(raw map[string]json.RawMessage) CLIFlags {
	flags := CLIFlags{Raw: cloneRaw(raw)}
	_ = json.Unmarshal(raw["accountValidation"], &flags.AccountValidation)
	_ = json.Unmarshal(raw["basicAuth"], &flags.BasicAuth)
	_ = json.Unmarshal(raw["os"], &flags.OS)
	_ = json.Unmarshal(raw["webhooks"], &flags.Webhooks)
	_ = json.Unmarshal(raw["disabledWebhooks"], &flags.DisabledWebhooks)
	_ = json.Unmarshal(raw["autoMarkRead"], &flags.AutoMarkRead)
	_ = json.Unmarshal(raw["autoReply"], &flags.AutoReply)
	_ = json.Unmarshal(raw["basePath"], &flags.BasePath)
	_ = json.Unmarshal(raw["debug"], &flags.Debug)
	_ = json.Unmarshal(raw["webhookSecret"], &flags.WebhookSecret)
	return flags
}

func mergeEnvValue(env map[string]string, value any) {
	switch typed := value.(type) {
	case map[string]string:
		for key, value := range typed {
			env[key] = value
		}
	case map[string]any:
		for key, value := range typed {
			if text, ok := value.(string); ok {
				env[key] = text
			}
		}
	case string:
		for _, pair := range strings.Fields(typed) {
			key, value, ok := strings.Cut(pair, "=")
			if ok && key != "" {
				env[key] = value
			}
		}
	}
}

func mergeCreateConfig(base InstanceConfig, user InstanceConfig) InstanceConfig {
	merged := InstanceConfig{Raw: cloneRaw(base.Raw), Args: base.Args, Flags: base.Flags}
	for key, value := range user.Raw {
		merged.Raw[key] = value
	}
	if user.Args != nil {
		merged.Args = user.Args
	}
	if user.Env != nil {
		merged.Env = user.Env
	}
	if user.EnvVars != nil {
		merged.EnvVars = user.EnvVars
	}
	merged.Flags = mergeFlags(base.Flags, user.Flags)
	return merged
}

func mergeFlags(base CLIFlags, user CLIFlags) CLIFlags {
	merged := base
	merged.Raw = cloneRaw(base.Raw)
	for key, value := range user.Raw {
		merged.Raw[key] = append(json.RawMessage(nil), value...)
	}
	if user.AccountValidation != nil {
		merged.AccountValidation = user.AccountValidation
	}
	if user.BasicAuth != nil {
		merged.BasicAuth = user.BasicAuth
	}
	if user.OS != "" {
		merged.OS = user.OS
	}
	if user.Webhooks != nil {
		merged.Webhooks = user.Webhooks
	}
	if user.DisabledWebhooks != nil {
		merged.DisabledWebhooks = user.DisabledWebhooks
	}
	if user.AutoMarkRead != nil {
		merged.AutoMarkRead = user.AutoMarkRead
	}
	if user.AutoReply != "" {
		merged.AutoReply = user.AutoReply
	}
	if user.BasePath != "" {
		merged.BasePath = user.BasePath
	}
	if user.Debug != nil {
		merged.Debug = user.Debug
	}
	if user.WebhookSecret != "" {
		merged.WebhookSecret = user.WebhookSecret
	}
	return merged
}

func configToJSON(config InstanceConfig) string {
	raw := cloneRaw(config.Raw)
	if config.Args != nil {
		raw["args"] = mustMarshal(config.Args)
	}
	if config.Env != nil {
		raw["env"] = mustMarshal(config.Env)
	}
	if config.EnvVars != nil {
		raw["envVars"] = mustMarshal(config.EnvVars)
	}
	raw["flags"] = flagsToRaw(config.Flags)
	encoded, _ := json.Marshal(raw)
	return string(encoded)
}

func flagsToRaw(flags CLIFlags) json.RawMessage {
	raw := cloneRaw(flags.Raw)
	if flags.AccountValidation != nil {
		raw["accountValidation"] = mustMarshal(flags.AccountValidation)
	}
	if flags.BasicAuth != nil {
		raw["basicAuth"] = mustMarshal(flags.BasicAuth)
	}
	if flags.OS != "" {
		raw["os"] = mustMarshal(flags.OS)
	}
	if flags.Webhooks != nil {
		raw["webhooks"] = mustMarshal(flags.Webhooks)
	}
	if flags.DisabledWebhooks != nil {
		raw["disabledWebhooks"] = mustMarshal(flags.DisabledWebhooks)
	}
	if flags.AutoMarkRead != nil {
		raw["autoMarkRead"] = mustMarshal(flags.AutoMarkRead)
	}
	if flags.AutoReply != "" {
		raw["autoReply"] = mustMarshal(flags.AutoReply)
	}
	if flags.BasePath != "" {
		raw["basePath"] = mustMarshal(flags.BasePath)
	}
	if flags.Debug != nil {
		raw["debug"] = mustMarshal(flags.Debug)
	}
	if flags.WebhookSecret != "" {
		raw["webhookSecret"] = mustMarshal(flags.WebhookSecret)
	}
	return mustMarshal(raw)
}

func cloneRaw(raw map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := map[string]json.RawMessage{}
	for key, value := range raw {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}

func mustMarshal(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}
