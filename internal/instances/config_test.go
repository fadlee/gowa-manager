package instances

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestConfigParseConfigReturnsEmptyForInvalidOrEmptyJSON(t *testing.T) {
	if got := ParseConfig(""); !reflect.DeepEqual(got.Raw, map[string]json.RawMessage{}) {
		t.Fatalf("ParseConfig empty Raw = %#v, want empty", got.Raw)
	}

	if got := ParseConfig("{bad-json"); !reflect.DeepEqual(got.Raw, map[string]json.RawMessage{}) {
		t.Fatalf("ParseConfig invalid Raw = %#v, want empty", got.Raw)
	}
}

func TestConfigParseConfigPreservesValidJSON(t *testing.T) {
	config := ParseConfig(`{"args":["rest","--port=PORT"],"env":{"LOG_LEVEL":"debug"}}`)

	if got := ProcessArgs(config, 8123); !reflect.DeepEqual(got, []string{"rest", "--port=8123"}) {
		t.Fatalf("ProcessArgs = %#v", got)
	}

	env := ParseEnvironmentVars(config, 8123, nil)
	if env["LOG_LEVEL"] != "debug" {
		t.Fatalf("LOG_LEVEL = %q, want debug", env["LOG_LEVEL"])
	}
}

func TestConfigDefaultConfig(t *testing.T) {
	got := DefaultConfig()
	if !reflect.DeepEqual(got.Args, []string{"rest", "--port=PORT"}) {
		t.Fatalf("DefaultConfig Args = %#v", got.Args)
	}
	if got.Flags.AccountValidation == nil || *got.Flags.AccountValidation != true {
		t.Fatalf("DefaultConfig accountValidation = %#v, want true", got.Flags.AccountValidation)
	}
	if got.Flags.OS != "GowaManager" {
		t.Fatalf("DefaultConfig os = %q, want GowaManager", got.Flags.OS)
	}
}

func TestConfigFlagsToArgs(t *testing.T) {
	accountValidation := false
	autoMarkRead := true
	debug := true
	args := FlagsToArgs(CLIFlags{
		AccountValidation: &accountValidation,
		BasicAuth: []BasicAuthPair{
			{Username: "admin", Password: "secret"},
			{Username: "api", Password: "key:with:colon"},
		},
		OS:            "Chrome",
		Webhooks:      []string{"https://example.com/a", "https://example.com/b"},
		AutoMarkRead:  &autoMarkRead,
		AutoReply:     "hello world",
		BasePath:      "/app/ABC12345",
		Debug:         &debug,
		WebhookSecret: "hook-secret",
	})

	want := []string{
		"--account-validation=false",
		"--basic-auth=admin:secret",
		"--basic-auth=api:key:with:colon",
		"--os=Chrome",
		"--webhook=https://example.com/a",
		"--webhook=https://example.com/b",
		"--auto-mark-read=true",
		"--autoreply=hello world",
		"--base-path=/app/ABC12345",
		"--debug=true",
		"--webhook-secret=hook-secret",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("FlagsToArgs = %#v, want %#v", args, want)
	}
}

func TestConfigFlagsToArgsExcludesDisabledWebhooks(t *testing.T) {
	args := FlagsToArgs(CLIFlags{
		Webhooks:         []string{"https://example.com/a", "https://example.com/b", "https://example.com/c"},
		DisabledWebhooks: []string{"https://example.com/b"},
	})

	want := []string{"--webhook=https://example.com/a", "--webhook=https://example.com/c"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("FlagsToArgs = %#v, want %#v", args, want)
	}
}

func TestConfigFlagsToArgsExcludesAllDisabledWebhooks(t *testing.T) {
	args := FlagsToArgs(CLIFlags{
		Webhooks:         []string{"https://example.com/a", "https://example.com/b"},
		DisabledWebhooks: []string{"https://example.com/a", "https://example.com/b"},
	})

	if len(args) != 0 {
		t.Fatalf("FlagsToArgs = %#v, want no webhook args", args)
	}
}

func TestConfigProcessArgs(t *testing.T) {
	debug := false
	config := InstanceConfig{
		Args: []string{"rest", "--port=PORT"},
		Flags: CLIFlags{
			BasePath: "/app/ABC12345",
			Debug:    &debug,
		},
	}

	args := ProcessArgs(config, 8123)
	want := []string{"rest", "--port=8123", "--base-path=/app/ABC12345", "--debug=false"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("ProcessArgs = %#v, want %#v", args, want)
	}
}

func TestConfigProcessStringArgs(t *testing.T) {
	args := ProcessArgs(InstanceConfig{Args: "rest --port=PORT --debug=true"}, 8124)
	want := []string{"rest", "--port=8124", "--debug=true"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("ProcessArgs string = %#v, want %#v", args, want)
	}

	args = ProcessArgs(InstanceConfig{Args: "   "}, 8125)
	if len(args) != 0 {
		t.Fatalf("ProcessArgs blank = %#v, want empty", args)
	}
}

func TestConfigParseEnvironmentVars(t *testing.T) {
	env := ParseEnvironmentVars(ParseConfig(`{"env":{"PORT":"9999","LOG_LEVEL":"debug"}}`), 8126, nil)
	if env["PORT"] != "9999" || env["LOG_LEVEL"] != "debug" {
		t.Fatalf("env object = %#v", env)
	}

	env = ParseEnvironmentVars(ParseConfig(`{"env":"TOKEN=a=b=c LOG_LEVEL=info"}`), 8127, nil)
	if env["PORT"] != "8127" || env["TOKEN"] != "a=b=c" || env["LOG_LEVEL"] != "info" {
		t.Fatalf("env string = %#v", env)
	}

	env = ParseEnvironmentVars(ParseConfig(`{"envVars":"LEGACY=yes SECRET=x=y"}`), 8128, map[string]string{"BASE": "ok"})
	if env["PORT"] != "8128" || env["LEGACY"] != "yes" || env["SECRET"] != "x=y" || env["BASE"] != "ok" {
		t.Fatalf("legacy envVars = %#v", env)
	}
}

func TestConfigNormalizeUpdateConfig(t *testing.T) {
	config := NormalizeUpdateConfig(
		`{"flags":{"basePath":"/app/OLDKEY","debug":true}}`,
		ptrString(`{"flags":{"basePath":"/custom/path","debug":false},"env":{"FOO":"bar"}}`),
		"ABC12345",
	)

	assertJSONEqual(t, config, `{"flags":{"basePath":"/app/ABC12345","debug":false},"env":{"FOO":"bar"}}`)
}

func TestConfigNormalizeUpdateConfigUsesExistingConfigWhenNextConfigIsUndefined(t *testing.T) {
	config := NormalizeUpdateConfig(
		`{"flags":{"basePath":"/app/OLDKEY","debug":true},"custom":{"keep":true}}`,
		nil,
		"EXISTING",
	)

	assertJSONEqual(t, config, `{"flags":{"basePath":"/app/EXISTING","debug":true},"custom":{"keep":true}}`)
}

func TestConfigNormalizeUpdateConfigFallbacks(t *testing.T) {
	assertJSONEqual(t,
		NormalizeUpdateConfig(`{"flags":{"basePath":"/app/OLDKEY"},"command":"rest"}`, ptrString("{bad-json"), "GOODKEY1"),
		`{"flags":{"basePath":"/app/GOODKEY1"},"command":"rest"}`,
	)

	assertJSONEqual(t,
		NormalizeUpdateConfig(`{}`, ptrString(`{"command":"rest"}`), "NOFLAGS1"),
		`{"command":"rest","flags":{"basePath":"/app/NOFLAGS1"}}`,
	)

	assertJSONEqual(t,
		NormalizeUpdateConfig(`{}`, ptrString(`["invalid"]`), "ARRAYKEY"),
		`{"flags":{"basePath":"/app/ARRAYKEY"}}`,
	)
}

func TestConfigBuildCreateConfig(t *testing.T) {
	assertJSONEqual(t,
		BuildCreateConfig(ptrString(`{"flags":{"basePath":"/wrong","os":"Chrome","unknownFlag":{"nested":true}},"custom":{"keep":true}}`), "CREATE01"),
		`{"args":["rest","--port=PORT"],"flags":{"accountValidation":true,"os":"Chrome","basePath":"/app/CREATE01","unknownFlag":{"nested":true}},"custom":{"keep":true}}`,
	)

	assertJSONEqual(t,
		BuildCreateConfig(ptrString(`{bad-json`), "CREATE02"),
		`{"args":["rest","--port=PORT"],"flags":{"accountValidation":true,"os":"GowaManager","basePath":"/app/CREATE02"}}`,
	)
}

func FuzzNormalizeUpdateConfigRestoresBasePath(f *testing.F) {
	f.Add(`{"flags":{"basePath":"/wrong"},"unknown":{"keep":true}}`, "FUZZKEY1")
	f.Add(`{"flags":{"basePath":"/other","debug":false,"unknownFlag":"keep"},"extra":"value"}`, "FUZZKEY3")
	f.Add(`{"flags":{"basePath":"/nested"},"unknownList":[1,{"two":true}],"unknownBool":false}`, "FUZZKEY4")
	f.Add(`[]`, "FUZZKEY2")
	f.Fuzz(func(t *testing.T, next string, key string) {
		if key == "" {
			key = "KEY"
		}
		var input map[string]json.RawMessage
		validObjectWithUnknowns := json.Unmarshal([]byte(next), &input) == nil && input != nil
		got := NormalizeUpdateConfig(`{"unknown":"existing"}`, &next, key)
		var decoded map[string]any
		if err := json.Unmarshal([]byte(got), &decoded); err != nil {
			t.Fatalf("normalized config is invalid JSON: %v", err)
		}
		flags, ok := decoded["flags"].(map[string]any)
		if !ok {
			t.Fatalf("flags missing from %#v", decoded)
		}
		if flags["basePath"] != "/app/"+key {
			t.Fatalf("basePath = %#v, want %q", flags["basePath"], "/app/"+key)
		}
		if !validObjectWithUnknowns {
			return
		}
		for field, value := range input {
			if field == "flags" {
				continue
			}
			gotValue, ok := decoded[field]
			if !ok {
				t.Fatalf("unknown field %q was deleted: %#v", field, decoded)
			}
			var wantValue any
			if err := json.Unmarshal(value, &wantValue); err != nil {
				t.Fatalf("seed field %q is invalid JSON: %v", field, err)
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				t.Fatalf("unknown field %q = %#v, want %#v", field, gotValue, wantValue)
			}
		}
		var inputFlags map[string]json.RawMessage
		if err := json.Unmarshal(input["flags"], &inputFlags); err != nil || inputFlags == nil {
			return
		}
		for field, value := range inputFlags {
			if field == "basePath" {
				continue
			}
			gotValue, ok := flags[field]
			if !ok {
				t.Fatalf("unknown flags field %q was deleted: %#v", field, flags)
			}
			var wantValue any
			if err := json.Unmarshal(value, &wantValue); err != nil {
				t.Fatalf("seed flags field %q is invalid JSON: %v", field, err)
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				t.Fatalf("unknown flags field %q = %#v, want %#v", field, gotValue, wantValue)
			}
		}
	})
}

func ptrString(value string) *string {
	return &value
}

func assertJSONEqual(t *testing.T, got string, want string) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("got invalid JSON %q: %v", got, err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("want invalid JSON %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON = %#v, want %#v", gotValue, wantValue)
	}
}
