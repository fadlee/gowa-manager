package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
)

// validMagicCookie builds a cookie header carrying a valid magic admin
// token for the given instance key, so auth-injection tests can exercise
// the happy path without reproducing the signing internals.
func validMagicCookie(t *testing.T, svc *auth.MagicAuthService, key string) string {
	t.Helper()
	token, _ := svc.CreateToken(key, time.Now())
	return svc.CookieName(key) + "=" + url.QueryEscape(token)
}

// ---- GetInstanceBasicAuthHeader ----

func TestGetInstanceBasicAuthHeader_FirstAuth(t *testing.T) {
	cfg := `{"flags":{"basicAuth":[{"username":"admin","password":"secret"}]}}`
	inst := instances.Instance{Key: "k", Config: cfg}
	got := GetInstanceBasicAuthHeader(inst)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))
	if got != expected {
		t.Fatalf("got %q, want %q", got, expected)
	}
}

func TestGetInstanceBasicAuthHeader_NoConfig(t *testing.T) {
	inst := instances.Instance{Key: "k", Config: ""}
	if got := GetInstanceBasicAuthHeader(inst); got != "" {
		t.Fatalf("expected empty auth header, got %q", got)
	}
}

func TestGetInstanceBasicAuthHeader_NoBasicAuth(t *testing.T) {
	inst := instances.Instance{Key: "k", Config: `{"flags":{}}`}
	if got := GetInstanceBasicAuthHeader(inst); got != "" {
		t.Fatalf("expected empty auth header, got %q", got)
	}
}

func TestGetInstanceBasicAuthHeader_MissingUsername(t *testing.T) {
	inst := instances.Instance{Key: "k", Config: `{"flags":{"basicAuth":[{"password":"secret"}]}}`}
	if got := GetInstanceBasicAuthHeader(inst); got != "" {
		t.Fatalf("expected empty auth header when username missing, got %q", got)
	}
}

func TestGetInstanceBasicAuthHeader_InvalidJSON(t *testing.T) {
	inst := instances.Instance{Key: "k", Config: `{not json`}
	if got := GetInstanceBasicAuthHeader(inst); got != "" {
		t.Fatalf("expected empty auth header on invalid json, got %q", got)
	}
}

func TestGetInstanceBasicAuthHeader_SecondAuthIgnored(t *testing.T) {
	// Only the first basicAuth entry is used.
	cfg := `{"flags":{"basicAuth":[{"username":"first","password":"pw1"},{"username":"second","password":"pw2"}]}}`
	inst := instances.Instance{Key: "k", Config: cfg}
	got := GetInstanceBasicAuthHeader(inst)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("first:pw1"))
	if got != expected {
		t.Fatalf("got %q, want %q", got, expected)
	}
}

// ---- PrepareForwardHeaders: X-Forwarded-* ----

func TestPrepareForwardHeaders_AddsForwardedHeaders(t *testing.T) {
	orig := http.Header{}
	orig.Set("Host", "manager.example.com")
	orig.Set("Cookie", "foo=bar")
	inst := instances.Instance{Key: "k", Config: "{}"}
	svc := auth.NewMagicAuthServiceWithSecret("s")

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://manager.example.com/app/k/"))

	if v := h.Get("X-Forwarded-For"); v != "localhost" {
		t.Fatalf("X-Forwarded-For = %q, want localhost", v)
	}
	if v := h.Get("X-Forwarded-Proto"); v != "http" {
		t.Fatalf("X-Forwarded-Proto = %q, want http", v)
	}
	if v := h.Get("X-Forwarded-Host"); v != "manager.example.com" {
		t.Fatalf("X-Forwarded-Host = %q, want manager.example.com", v)
	}
}

func TestPrepareForwardHeaders_PreservesExistingForwardedFor(t *testing.T) {
	orig := http.Header{}
	orig.Set("X-Forwarded-For", "203.0.113.5")
	inst := instances.Instance{Key: "k", Config: "{}"}
	svc := auth.NewMagicAuthServiceWithSecret("s")

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	if v := h.Get("X-Forwarded-For"); v != "203.0.113.5" {
		t.Fatalf("X-Forwarded-For = %q, want 203.0.113.5", v)
	}
}

func TestPrepareForwardHeaders_DefaultHostWhenAbsent(t *testing.T) {
	orig := http.Header{}
	inst := instances.Instance{Key: "k", Config: "{}"}
	svc := auth.NewMagicAuthServiceWithSecret("s")

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	if v := h.Get("X-Forwarded-Host"); v != "localhost" {
		t.Fatalf("X-Forwarded-Host = %q, want localhost", v)
	}
}

// ---- PrepareForwardHeaders: Host removal / hop-by-hop ----

func TestPrepareForwardHeaders_RemovesHost(t *testing.T) {
	orig := http.Header{}
	orig.Set("Host", "manager.example.com")
	inst := instances.Instance{Key: "k", Config: "{}"}
	svc := auth.NewMagicAuthServiceWithSecret("s")

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	if _, ok := h["Host"]; ok {
		t.Fatalf("Host header should be removed, got %v", h["Host"])
	}
}

func TestPrepareForwardHeaders_RemovesHopByHop(t *testing.T) {
	hopByHop := []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "TE", "Trailers",
		"Transfer-Encoding", "Upgrade",
	}
	orig := http.Header{}
	for _, h := range hopByHop {
		orig.Set(h, "value")
	}
	inst := instances.Instance{Key: "k", Config: "{}"}
	svc := auth.NewMagicAuthServiceWithSecret("s")

	out := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	for _, h := range hopByHop {
		if _, ok := out[http.CanonicalHeaderKey(h)]; ok {
			t.Fatalf("hop-by-hop header %q should be removed", h)
		}
	}
}

// ---- PrepareForwardHeaders: does not mutate original ----

func TestPrepareForwardHeaders_DoesNotMutateOriginal(t *testing.T) {
	orig := http.Header{}
	orig.Set("Host", "manager.example.com")
	orig.Set("Connection", "keep-alive")
	orig.Set("X-Custom", "keepme")
	inst := instances.Instance{Key: "k", Config: "{}"}
	svc := auth.NewMagicAuthServiceWithSecret("s")

	_ = PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	if orig.Get("Host") != "manager.example.com" {
		t.Fatalf("original Host mutated: %q", orig.Get("Host"))
	}
	if orig.Get("Connection") != "keep-alive" {
		t.Fatalf("original Connection mutated: %q", orig.Get("Connection"))
	}
	if _, ok := orig["X-Forwarded-For"]; ok {
		t.Fatal("original headers should not gain X-Forwarded-For")
	}
}

func TestPrepareForwardHeaders_PreservesCustomHeaders(t *testing.T) {
	orig := http.Header{}
	orig.Set("X-Custom", "keepme")
	inst := instances.Instance{Key: "k", Config: "{}"}
	svc := auth.NewMagicAuthServiceWithSecret("s")

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	if v := h.Get("X-Custom"); v != "keepme" {
		t.Fatalf("X-Custom = %q, want keepme", v)
	}
}

// ---- PrepareForwardHeaders: instance auth injection ----

func TestPrepareForwardHeaders_InjectsAuthWhenMagicCookieValid(t *testing.T) {
	svc := auth.NewMagicAuthServiceWithSecret("s")
	inst := instances.Instance{
		Key:    "k",
		Config: `{"flags":{"basicAuth":[{"username":"admin","password":"secret"}]}}`,
	}
	cookie := validMagicCookie(t, svc, "k")
	orig := http.Header{}
	orig.Set("Cookie", cookie)

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))
	if v := h.Get("Authorization"); v != expected {
		t.Fatalf("Authorization = %q, want %q", v, expected)
	}
}

func TestPrepareForwardHeaders_DoesNotInjectWhenAuthorizationPresent(t *testing.T) {
	svc := auth.NewMagicAuthServiceWithSecret("s")
	inst := instances.Instance{
		Key:    "k",
		Config: `{"flags":{"basicAuth":[{"username":"admin","password":"secret"}]}}`,
	}
	cookie := validMagicCookie(t, svc, "k")
	orig := http.Header{}
	orig.Set("Cookie", cookie)
	orig.Set("Authorization", "Bearer existing")

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	if v := h.Get("Authorization"); v != "Bearer existing" {
		t.Fatalf("Authorization = %q, want Bearer existing", v)
	}
}

func TestPrepareForwardHeaders_DoesNotInjectWhenMagicCookieInvalid(t *testing.T) {
	svc := auth.NewMagicAuthServiceWithSecret("s")
	inst := instances.Instance{
		Key:    "k",
		Config: `{"flags":{"basicAuth":[{"username":"admin","password":"secret"}]}}`,
	}
	orig := http.Header{}
	orig.Set("Cookie", "gowa_admin_auth_k=garbage")

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	if v := h.Get("Authorization"); v != "" {
		t.Fatalf("Authorization = %q, want empty (no valid magic cookie)", v)
	}
}

func TestPrepareForwardHeaders_DoesNotInjectWhenNoBasicAuthConfigured(t *testing.T) {
	svc := auth.NewMagicAuthServiceWithSecret("s")
	inst := instances.Instance{Key: "k", Config: "{}"}
	cookie := validMagicCookie(t, svc, "k")
	orig := http.Header{}
	orig.Set("Cookie", cookie)

	h := PrepareForwardHeaders(orig, inst, svc, mustParseURL(t, "http://m/app/k/"))

	if v := h.Get("Authorization"); v != "" {
		t.Fatalf("Authorization = %q, want empty (no basic auth configured)", v)
	}
}

func TestPrepareForwardHeaders_NilMagicAuthSkipsInjection(t *testing.T) {
	inst := instances.Instance{
		Key:    "k",
		Config: `{"flags":{"basicAuth":[{"username":"admin","password":"secret"}]}}`,
	}
	orig := http.Header{}
	orig.Set("Cookie", "anything")

	h := PrepareForwardHeaders(orig, inst, nil, mustParseURL(t, "http://m/app/k/"))

	if v := h.Get("Authorization"); v != "" {
		t.Fatalf("Authorization = %q, want empty when magicAuth is nil", v)
	}
}

// ---- IsBinaryContent ----

func TestIsBinaryContent(t *testing.T) {
	binary := []string{
		"image/png",
		"image/jpeg",
		"video/mp4",
		"audio/mpeg",
		"application/pdf",
		"application/zip",
		"application/octet-stream",
		"font/woff2",
		"application/font-woff",
	}
	for _, ct := range binary {
		if !IsBinaryContent(ct) {
			t.Fatalf("IsBinaryContent(%q) = false, want true", ct)
		}
	}
}

func TestIsBinaryContent_NonBinary(t *testing.T) {
	nonBinary := []string{
		"text/html",
		"application/json",
		"application/javascript",
		"text/css",
		"text/plain",
		"",
	}
	for _, ct := range nonBinary {
		if IsBinaryContent(ct) {
			t.Fatalf("IsBinaryContent(%q) = true, want false", ct)
		}
	}
}

func TestIsBinaryContent_WithCharset(t *testing.T) {
	// Content type with parameters should still be detected.
	if !IsBinaryContent("image/png; charset=utf-8") {
		t.Fatal("IsBinaryContent(image/png; charset=utf-8) = false, want true")
	}
}

// ---- StripAbsoluteURLs ----

func TestStripAbsoluteURLs_Object(t *testing.T) {
	in := []byte(`{"url":"http://localhost:8080/admin","name":"x"}`)
	out := StripAbsoluteURLs(in)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if m["url"] != "/admin" {
		t.Fatalf("url = %v, want /admin", m["url"])
	}
	if m["name"] != "x" {
		t.Fatalf("name = %v, want x", m["name"])
	}
}

func TestStripAbsoluteURLs_WithQueryAndHash(t *testing.T) {
	in := []byte(`{"url":"https://example.com/path?q=1#frag"}`)
	out := StripAbsoluteURLs(in)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if m["url"] != "/path?q=1#frag" {
		t.Fatalf("url = %v, want /path?q=1#frag", m["url"])
	}
}

func TestStripAbsoluteURLs_Array(t *testing.T) {
	in := []byte(`["http://a.com/x","https://b.com/y"]`)
	out := StripAbsoluteURLs(in)
	var arr []any
	if err := json.Unmarshal(out, &arr); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if arr[0] != "/x" || arr[1] != "/y" {
		t.Fatalf("arr = %v, want [/x /y]", arr)
	}
}

func TestStripAbsoluteURLs_Nested(t *testing.T) {
	in := []byte(`{"a":{"b":{"url":"http://h.com/p?x=1"}}}`)
	out := StripAbsoluteURLs(in)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	a := m["a"].(map[string]any)
	b := a["b"].(map[string]any)
	if b["url"] != "/p?x=1" {
		t.Fatalf("nested url = %v, want /p?x=1", b["url"])
	}
}

func TestStripAbsoluteURLs_LeavesNonURLStrings(t *testing.T) {
	in := []byte(`{"a":"not a url","b":"/relative","c":"ftp://x.com/y"}`)
	out := StripAbsoluteURLs(in)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if m["a"] != "not a url" {
		t.Fatalf("a = %v, want 'not a url'", m["a"])
	}
	if m["b"] != "/relative" {
		t.Fatalf("b = %v, want /relative", m["b"])
	}
	if m["c"] != "ftp://x.com/y" {
		t.Fatalf("c = %v, want ftp://x.com/y (only http/https stripped)", m["c"])
	}
}

func TestStripAbsoluteURLs_InvalidJSONReturnedUnchanged(t *testing.T) {
	in := []byte(`{not valid json`)
	out := StripAbsoluteURLs(in)
	if string(out) != string(in) {
		t.Fatalf("invalid json should be returned unchanged, got %q", string(out))
	}
}

func TestStripAbsoluteURLs_Primitives(t *testing.T) {
	in := []byte(`42`)
	out := StripAbsoluteURLs(in)
	if string(out) != "42" {
		t.Fatalf("primitive should be returned unchanged, got %q", string(out))
	}
}

func TestStripAbsoluteURLs_MalformedURLPreserved(t *testing.T) {
	// A string starting with http:// but not a valid URL is preserved.
	in := []byte(`{"url":"http://"}`)
	out := StripAbsoluteURLs(in)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if m["url"] != "http://" {
		t.Fatalf("malformed url = %v, want http://", m["url"])
	}
}

// ---- RewriteRedirectLocation ----

func TestRewriteRedirectLocation_LocalhostStripped(t *testing.T) {
	got := RewriteRedirectLocation("http://localhost:8080/admin", "mykey", "manager.example.com")
	if got != "/app/mykey/admin" {
		t.Fatalf("got %q, want /app/mykey/admin", got)
	}
}

func TestRewriteRedirectLocation_LocalhostWithQuery(t *testing.T) {
	got := RewriteRedirectLocation("http://localhost:8080/search?q=1", "mykey", "manager.example.com")
	if got != "/app/mykey/search?q=1" {
		t.Fatalf("got %q, want /app/mykey/search?q=1", got)
	}
}

// The proxy dials the upstream as 127.0.0.1, so GOWA can echo that host
// into Host-derived redirects; those must be rewritten too.
func TestRewriteRedirectLocation_LoopbackIPStripped(t *testing.T) {
	got := RewriteRedirectLocation("http://127.0.0.1:8080/admin?q=1", "mykey", "manager.example.com")
	if got != "/app/mykey/admin?q=1" {
		t.Fatalf("got %q, want /app/mykey/admin?q=1", got)
	}
}

func TestRewriteRedirectLocation_BarePathPrefixed(t *testing.T) {
	got := RewriteRedirectLocation("/admin", "mykey", "manager.example.com")
	if got != "/app/mykey/admin" {
		t.Fatalf("got %q, want /app/mykey/admin", got)
	}
}

func TestRewriteRedirectLocation_AlreadyPrefixedUnchanged(t *testing.T) {
	got := RewriteRedirectLocation("/app/mykey/admin", "mykey", "manager.example.com")
	if got != "/app/mykey/admin" {
		t.Fatalf("got %q, want /app/mykey/admin", got)
	}
}

func TestRewriteRedirectLocation_ExternalURLUnchanged(t *testing.T) {
	got := RewriteRedirectLocation("https://external.com/page", "mykey", "manager.example.com")
	if got != "https://external.com/page" {
		t.Fatalf("external url should be unchanged, got %q", got)
	}
}

func TestRewriteRedirectLocation_RootPath(t *testing.T) {
	got := RewriteRedirectLocation("http://localhost:8080/", "mykey", "manager.example.com")
	if got != "/app/mykey/" {
		t.Fatalf("got %q, want /app/mykey/", got)
	}
}

// ---- RewriteSetCookie ----

func TestRewriteSetCookie_RewritesPath(t *testing.T) {
	in := "session=abc; Path=/; HttpOnly"
	got := RewriteSetCookie(in, "mykey", "manager.example.com")
	if !strings.Contains(got, "Path=/app/mykey") {
		t.Fatalf("got %q, want Path=/app/mykey", got)
	}
	if !strings.Contains(got, "session=abc") {
		t.Fatalf("cookie name/value should be preserved: %q", got)
	}
	if !strings.Contains(got, "HttpOnly") {
		t.Fatalf("HttpOnly should be preserved: %q", got)
	}
}

func TestRewriteSetCookie_RewritesSubPath(t *testing.T) {
	in := "token=xyz; Path=/admin"
	got := RewriteSetCookie(in, "mykey", "manager.example.com")
	if !strings.Contains(got, "Path=/app/mykey/admin") {
		t.Fatalf("got %q, want Path=/app/mykey/admin", got)
	}
}

func TestRewriteSetCookie_RemovesDomain(t *testing.T) {
	in := "session=abc; Path=/; Domain=localhost; HttpOnly"
	got := RewriteSetCookie(in, "mykey", "manager.example.com")
	if strings.Contains(strings.ToLower(got), "domain=") {
		t.Fatalf("Domain attribute should be removed: %q", got)
	}
}

func TestRewriteSetCookie_AddsPathWhenAbsent(t *testing.T) {
	in := "session=abc; HttpOnly"
	got := RewriteSetCookie(in, "mykey", "manager.example.com")
	if !strings.Contains(got, "Path=/app/mykey") {
		t.Fatalf("got %q, want Path=/app/mykey added", got)
	}
}

func TestRewriteSetCookie_PreservesOtherAttributes(t *testing.T) {
	in := "s=v; Path=/; Secure; SameSite=Lax; Max-Age=3600"
	got := RewriteSetCookie(in, "mykey", "manager.example.com")
	for _, attr := range []string{"Secure", "SameSite=Lax", "Max-Age=3600"} {
		if !strings.Contains(got, attr) {
			t.Fatalf("attribute %q should be preserved in %q", attr, got)
		}
	}
}

func TestRewriteSetCookie_Empty(t *testing.T) {
	if got := RewriteSetCookie("", "mykey", "manager.example.com"); got != "" {
		t.Fatalf("empty input should return empty, got %q", got)
	}
}

func TestRewriteSetCookie_CaseInsensitiveAttributes(t *testing.T) {
	in := "s=v; path=/; DOMAIN=localhost"
	got := RewriteSetCookie(in, "mykey", "manager.example.com")
	if !strings.Contains(got, "Path=/app/mykey") {
		t.Fatalf("got %q, want Path=/app/mykey (case-insensitive)", got)
	}
	if strings.Contains(strings.ToLower(got), "domain=") {
		t.Fatalf("Domain should be removed (case-insensitive): %q", got)
	}
}

// ---- helpers ----

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}

// ---- GetInstanceBasicAuthCredentials ----

func TestGetInstanceBasicAuthCredentials_FirstAuth(t *testing.T) {
	inst := instances.Instance{Config: `{"flags":{"basicAuth":[{"username":"admin","password":"secret"},{"username":"ignored","password":"ignored"}]}}`}

	username, password, ok := GetInstanceBasicAuthCredentials(inst)

	if !ok || username != "admin" || password != "secret" {
		t.Fatalf("credentials = (%q, %q, %t), want (admin, secret, true)", username, password, ok)
	}
}

func TestGetInstanceBasicAuthCredentials_NoConfig(t *testing.T) {
	inst := instances.Instance{Config: ""}
	if _, _, ok := GetInstanceBasicAuthCredentials(inst); ok {
		t.Fatal("expected ok=false when config is empty")
	}
}

func TestGetInstanceBasicAuthCredentials_NoBasicAuth(t *testing.T) {
	inst := instances.Instance{Config: `{"flags":{}}`}
	if _, _, ok := GetInstanceBasicAuthCredentials(inst); ok {
		t.Fatal("expected ok=false when no basicAuth entry")
	}
}

func TestGetInstanceBasicAuthCredentials_MissingUsername(t *testing.T) {
	inst := instances.Instance{Config: `{"flags":{"basicAuth":[{"password":"secret"}]}}`}
	if _, _, ok := GetInstanceBasicAuthCredentials(inst); ok {
		t.Fatal("expected ok=false when username missing")
	}
}

func TestGetInstanceBasicAuthCredentials_InvalidJSON(t *testing.T) {
	inst := instances.Instance{Config: `{not json`}
	if _, _, ok := GetInstanceBasicAuthCredentials(inst); ok {
		t.Fatal("expected ok=false on invalid json")
	}
}

// ---- isHTMLContentType ----

func TestIsHTMLContentType_AcceptsTextHTML(t *testing.T) {
	if !isHTMLContentType("text/html") {
		t.Error(`text/html should be HTML`)
	}
	if !isHTMLContentType("text/html; charset=utf-8") {
		t.Error(`text/html; charset=utf-8 should be HTML`)
	}
	if !isHTMLContentType("TEXT/HTML") {
		t.Error(`TEXT/HTML should be HTML (case-insensitive)`)
	}
}

func TestIsHTMLContentType_RejectsNonHTML(t *testing.T) {
	if isHTMLContentType("application/json") {
		t.Error(`application/json should not be HTML`)
	}
	if isHTMLContentType("") {
		t.Error(`empty content type should not be HTML`)
	}
	if isHTMLContentType("application/javascript") {
		t.Error(`application/javascript should not be HTML`)
	}
	if isHTMLContentType("text/htmlfoo") {
		t.Error(`text/htmlfoo should not be HTML`)
	}
	if isHTMLContentType("xtext/htmlx") {
		t.Error(`xtext/htmlx should not be HTML`)
	}
}

// ---- InjectGOWAUILocalStorageBootstrap ----

func TestInjectGOWAUILocalStorageBootstrap_InsertsEscapedProxyStateBeforeModuleScript(t *testing.T) {
	inst := instances.Instance{Key: "K1", Config: `{"flags":{"basicAuth":[{"username":"ad\"min","password":"pa</script>ss"}]}}`}
	html := []byte(`<!doctype html><html><head><script type="module">start()</script></head><body></body></html>`)

	got := string(InjectGOWAUILocalStorageBootstrap(html, "https://manager.example", inst))

	for _, want := range []string{
		`localStorage.setItem("gowa-ui.connection.v1"`,
		`https://manager.example/app/K1`,
		`localStorage.setItem("gowa-ui.device.v1"`,
		`"selectedDeviceId":"default"`,
		`"username":"ad\"min"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("injected HTML missing %q: %s", want, got)
		}
	}
	// The password contains "</script>" which must never appear as a
	// literal sequence inside the inline <script> block, otherwise it
	// would terminate the script element early. Go's json.Marshal escapes
	// '<' to \u003c, and sanitizeScriptJSON provides defense-in-depth.
	// Extract the bootstrap <script> block (from the first "<script>" to
	// its closing "</script>") and verify no "</script" appears inside
	// the block body before the closing tag.
	lower := strings.ToLower(got)
	scriptStart := strings.Index(lower, "<script>")
	if scriptStart < 0 {
		t.Fatal("missing bootstrap <script> opening tag")
	}
	closeIdx := strings.Index(lower[scriptStart+len("<script>"):], "</script>")
	if closeIdx < 0 {
		t.Fatal("missing bootstrap </script> closing tag")
	}
	blockBody := got[scriptStart+len("<script>") : scriptStart+len("<script>")+closeIdx]
	if strings.Contains(strings.ToLower(blockBody), "</script") {
		t.Fatalf("password </script> sequence not escaped in bootstrap block body: %s", blockBody)
	}
	bootstrapIdx := strings.Index(got, "gowa-ui.connection.v1")
	moduleIdx := strings.Index(got, `type="module"`)
	if bootstrapIdx < 0 || moduleIdx < 0 {
		t.Fatalf("missing bootstrap or module script: %s", got)
	}
	if bootstrapIdx > moduleIdx {
		t.Fatal("bootstrap must precede the module script")
	}
}

func TestInjectGOWAUILocalStorageBootstrap_InsertsAfterHeadWhenNoModuleScript(t *testing.T) {
	inst := instances.Instance{Key: "k", Config: `{"flags":{"basicAuth":[{"username":"u","password":"p"}]}}`}
	html := []byte(`<!doctype html><html><head><title>old</title></head><body></body></html>`)

	got := string(InjectGOWAUILocalStorageBootstrap(html, "http://manager.test", inst))

	headIdx := strings.Index(got, "<head>")
	bootstrapIdx := strings.Index(got, "gowa-ui.connection.v1")
	if headIdx < 0 || bootstrapIdx < 0 {
		t.Fatalf("missing head or bootstrap: %s", got)
	}
	if bootstrapIdx < headIdx {
		t.Fatal("bootstrap should be inserted after <head>")
	}
}

// Without Basic Auth the bootstrap still points GOWA UI at this instance,
// with empty credentials. Every instance shares the manager's origin and
// therefore its localStorage, so skipping it left the previous instance's
// connection in place and the admin page opened the wrong instance.
func TestInjectGOWAUILocalStorageBootstrap_ResetsConnectionWithoutCredentials(t *testing.T) {
	html := []byte(`<!doctype html><html><head></head><body>old ui</body></html>`)
	for _, config := range []string{`{"flags":{}}`, ``, `not json`} {
		inst := instances.Instance{Key: "K1", Config: config}

		got := string(InjectGOWAUILocalStorageBootstrap(html, "http://manager.test", inst))

		for _, want := range []string{
			`localStorage.setItem("gowa-ui.connection.v1"`,
			`"baseUrl":"http://manager.test/app/K1"`,
			`"username":""`,
			`"password":""`,
			`localStorage.setItem("gowa-ui.device.v1"`,
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("config %q: injected HTML missing %q: %s", config, want, got)
			}
		}
	}
}

func TestInjectGOWAUILocalStorageBootstrap_ReturnsOriginalWhenNoInsertionPoint(t *testing.T) {
	inst := instances.Instance{Key: "k", Config: `{"flags":{"basicAuth":[{"username":"u","password":"p"}]}}`}
	html := []byte(`plain text without html structure`)

	if got := InjectGOWAUILocalStorageBootstrap(html, "http://manager.test", inst); !bytes.Equal(got, html) {
		t.Fatalf("HTML changed when no insertion point exists: %s", got)
	}
}

func TestInjectGOWAUILocalStorageBootstrap_TrimTrailingSlashOnOrigin(t *testing.T) {
	inst := instances.Instance{Key: "k", Config: `{"flags":{"basicAuth":[{"username":"u","password":"p"}]}}`}
	html := []byte(`<!doctype html><html><head></head><body></body></html>`)

	got := string(InjectGOWAUILocalStorageBootstrap(html, "http://manager.test/", inst))

	if !strings.Contains(got, `"baseUrl":"http://manager.test/app/k"`) {
		t.Fatalf("expected trimmed origin in baseUrl: %s", got)
	}
}

func TestInjectGOWAUILocalStorageBootstrap_MalformedHTMLReturnsOriginal(t *testing.T) {
	inst := instances.Instance{Key: "k", Config: `{"flags":{"basicAuth":[{"username":"u","password":"p"}]}}`}
	// HTML with a <script> tag that has no closing '>' and no <head>.
	html := []byte(`<html><body><script type="module"`)

	if got := InjectGOWAUILocalStorageBootstrap(html, "http://manager.test", inst); !bytes.Equal(got, html) {
		t.Fatalf("malformed HTML should be returned unchanged: %s", got)
	}
}
