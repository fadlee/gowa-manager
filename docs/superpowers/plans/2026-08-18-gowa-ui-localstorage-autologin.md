# GOWA UI LocalStorage Auto-Login Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Open new GOWA UI releases through a magic admin link with their proxy connection and Basic Auth state automatically initialized in browser localStorage.

**Architecture:** Extend the Go reverse proxy response-rewrite stage with a narrowly scoped HTML bootstrap. After the existing magic-link redirect creates an instance-scoped magic cookie, only an eligible proxied HTML document gets a safely serialized script that writes the GOWA UI Zustand state using the manager-origin proxy URL. Existing magic-cookie Basic Auth injection remains unchanged and preserves older GOWA UI behavior.

**Tech Stack:** Go 1.24, `net/http`, `httputil.ReverseProxy`, Go standard library `encoding/json`, existing `internal/auth` magic-cookie service, Go test package.

## Global Constraints

- Do not branch behavior on a GOWA version string; the feature is additive and version-agnostic.
- Never put instance Basic Auth credentials in URLs, response headers, logs, manager API responses, or HTML without a valid instance-specific magic cookie.
- Serialize script data via `json.Marshal`; do not concatenate username or password into JavaScript literals.
- Preserve existing proxy behavior for non-HTML responses, ordinary proxy requests, invalid/expired magic links, missing credentials, JSON URL rewriting, and WebSocket forwarding.
- Keep explicit client `Authorization` headers higher priority than proxy-injected instance Basic Auth.
- Use Conventional Commit messages only; do not add AI attribution.

---

## File Structure

- Modify `internal/proxy/rewrite.go`
  - Parse the first configured Basic Auth pair without exposing it via an HTTP API.
  - Build an escaped localStorage bootstrap and insert it into eligible GOWA UI HTML.
- Modify `internal/proxy/http.go`
  - Give response rewriting the request URL, instance, and magic-auth service so it can make its eligibility decision.
  - Buffer HTML only when the request carries a valid magic cookie and credentials exist.
- Modify `internal/proxy/rewrite_test.go`
  - Unit-test credential extraction and HTML bootstrap generation/insertion, including escaping and no-op paths.
- Modify `internal/proxy/http_test.go`
  - Exercise the reverse proxy end to end for valid-cookie HTML injection and unchanged backward-compatible paths.

## Task 1: Add Safe Credential and HTML Bootstrap Helpers

**Files:**
- Modify: `internal/proxy/rewrite.go`
- Modify: `internal/proxy/rewrite_test.go`

**Interfaces:**
- Consumes: `instances.Instance.Config` containing `flags.basicAuth[0].username` and `.password`.
- Produces:
  - `func GetInstanceBasicAuthCredentials(instance instances.Instance) (username string, password string, ok bool)`
  - `func InjectGOWAUILocalStorageBootstrap(html []byte, managerOrigin string, instance instances.Instance) []byte`
  - `func isHTMLContentType(contentType string) bool`
- Existing `GetInstanceBasicAuthHeader` must reuse `GetInstanceBasicAuthCredentials` so both header injection and browser bootstrap select exactly the same first credential.

- [ ] **Step 1: Write failing helper tests**

Add tests in `internal/proxy/rewrite_test.go` that define the desired behavior:

```go
func TestGetInstanceBasicAuthCredentials_FirstAuth(t *testing.T) {
    inst := instances.Instance{Config: `{"flags":{"basicAuth":[{"username":"admin","password":"secret"},{"username":"ignored","password":"ignored"}]}}`}

    username, password, ok := GetInstanceBasicAuthCredentials(inst)

    if !ok || username != "admin" || password != "secret" {
        t.Fatalf("credentials = (%q, %q, %t), want (admin, secret, true)", username, password, ok)
    }
}

func TestInjectGOWAUILocalStorageBootstrap_InsertsEscapedProxyStateBeforeModuleScript(t *testing.T) {
    inst := instances.Instance{Key: "K1", Config: `{"flags":{"basicAuth":[{"username":"ad\"min","password":"pa</script>ss"}]}}`}
    html := []byte(`<!doctype html><html><head><script type="module">start()</script></head><body></body></html>`)

    got := string(InjectGOWAUILocalStorageBootstrap(html, "https://manager.example", inst))

    for _, want := range []string{
        `localStorage.setItem("gowa-ui.connection.v1"`,
        `https://manager.example/app/K1`,
        `localStorage.setItem("gowa-ui.device.v1"`,
        `"selectedDeviceId":"default"`,
        `"username":"ad\\\"min"`,
        `pa<\/script>ss`,
    } {
        if !strings.Contains(got, want) {
            t.Fatalf("injected HTML missing %q: %s", want, got)
        }
    }
    if strings.Index(got, "gowa-ui.connection.v1") > strings.Index(got, `type="module"`) {
        t.Fatal("bootstrap must precede the module script")
    }
}

func TestInjectGOWAUILocalStorageBootstrap_ReturnsOriginalForMissingCredentials(t *testing.T) {
    html := []byte(`<!doctype html><html><head></head><body>old ui</body></html>`)
    inst := instances.Instance{Key: "K1", Config: `{"flags":{}}`}

    if got := InjectGOWAUILocalStorageBootstrap(html, "http://manager.test", inst); !bytes.Equal(got, html) {
        t.Fatalf("HTML changed without credentials: %s", got)
    }
}
```

Also add tests that `isHTMLContentType` accepts `text/html` with parameters and rejects `application/json` and an empty content type. Name the production change that should make these tests fail: removing credential parsing, changing either localStorage key, rendering an unescaped script, or injecting after the first module script.

- [ ] **Step 2: Run the focused helper tests and verify RED**

Run:

```bash
go test ./internal/proxy -run 'Test(GetInstanceBasicAuthCredentials|InjectGOWAUILocalStorageBootstrap|IsHTMLContentType)' -count=1
```

Expected: compilation failure because the helper interfaces do not exist yet.

- [ ] **Step 3: Implement minimal credential extraction and bootstrap insertion**

In `internal/proxy/rewrite.go`:

1. Add `GetInstanceBasicAuthCredentials` that unmarshals only the existing nested `flags.basicAuth` shape; return `("", "", false)` for empty config, invalid JSON, an empty slice, or empty first username/password.
2. Refactor `GetInstanceBasicAuthHeader` to call that function and encode only when `ok` is true.
3. Add `isHTMLContentType` using a case-insensitive `text/html` content-type check.
4. Build these exact persisted values with `json.Marshal`:

```go
connection := struct {
    State struct {
        BaseURL  string  `json:"baseUrl"`
        Username string  `json:"username"`
        Password string  `json:"password"`
    } `json:"state"`
    Version int `json:"version"`
}{Version: 0}
connection.State.BaseURL = strings.TrimRight(managerOrigin, "/") + "/app/" + instance.Key
connection.State.Username = username
connection.State.Password = password

device := struct {
    State struct {
        SelectedDeviceID string `json:"selectedDeviceId"`
    } `json:"state"`
    Version int `json:"version"`
}{Version: 0}
device.State.SelectedDeviceID = "default"
```

5. Insert a single inline `<script>` before the first `<script type="module"` occurrence. If no module script exists, insert immediately after the opening `<head>` tag. If neither insertion point is found, return the original bytes.
6. Defend against HTML parser termination by replacing every case-insensitive `</script` sequence in marshaled JSON before placing it in the script body. Do not log either JSON payload.

The generated JavaScript must only invoke `localStorage.setItem` with the two fixed key names and the marshaled state values.

- [ ] **Step 4: Run the focused helper tests and verify GREEN**

Run:

```bash
go test ./internal/proxy -run 'Test(GetInstanceBasicAuthCredentials|InjectGOWAUILocalStorageBootstrap|IsHTMLContentType|GetInstanceBasicAuthHeader)' -count=1
```

Expected: PASS. Confirm the old Basic Auth header tests still pass, proving the extraction refactor preserved header behavior.

- [ ] **Step 5: Commit the focused helper change**

```bash
git add internal/proxy/rewrite.go internal/proxy/rewrite_test.go
git commit -m "feat(proxy): build gowa-ui connection bootstrap"
```

## Task 2: Inject Bootstrap Only for Authorized HTML Proxy Responses

**Files:**
- Modify: `internal/proxy/http.go`
- Modify: `internal/proxy/http_test.go`

**Interfaces:**
- Consumes:
  - `InjectGOWAUILocalStorageBootstrap([]byte, string, instances.Instance) []byte` from Task 1.
  - `auth.MagicAuthService.HasValidCookie(cookieHeader, instanceKey, time.Now())`.
  - Request URL and Host received by `HTTPProxy.ServeHTTP`.
- Produces:
  - `modifyProxyResponse` receives enough context to preserve current redirect/cookie/JSON transformations and conditionally replace an eligible HTML body with the bootstrap-injected body.

- [ ] **Step 1: Write failing end-to-end proxy tests**

Extend the upstream test fixture in `internal/proxy/http_test.go` with a deterministic HTML endpoint, for example `/ui`:

```go
mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    _, _ = io.WriteString(w, `<!doctype html><html><head><script type="module">window.gowaUI = true</script></head><body>GOWA UI</body></html>`)
})
```

Write these tests against `newProxyServer`:

```go
func TestHTTPProxy_InjectsGOWAUIBootstrapForValidMagicCookie(t *testing.T) {
    upstream := newUpstreamServer(t)
    inst := newRunningInstance("k", upstreamPort(t, upstream), basicAuthConfig("admin", "secret"))
    svc := auth.NewMagicAuthServiceWithSecret("s")
    srv := newProxyServer(t, svc, nil, inst)

    req, err := http.NewRequest(http.MethodGet, proxyURL(srv.URL, "k", "/ui"), nil)
    if err != nil { t.Fatal(err) }
    req.Header.Set("Cookie", validMagicCookie(t, svc, "k"))
    resp, err := http.DefaultClient.Do(req)
    if err != nil { t.Fatal(err) }
    defer resp.Body.Close()
    body, _ := io.ReadAll(resp.Body)

    for _, want := range []string{
        `gowa-ui.connection.v1`,
        `gowa-ui.device.v1`,
        srv.URL + `/app/k`,
        `"username":"admin"`,
        `"password":"secret"`,
    } {
        if !strings.Contains(string(body), want) { t.Fatalf("body missing %q", want) }
    }
}
```

Add separate tests proving unchanged response bodies for: no magic cookie, invalid magic cookie, and a valid magic cookie with an instance that has no Basic Auth configuration. Add a JSON `/echo` valid-cookie assertion that the response still remains valid JSON and has no bootstrap key, preserving prior JSON behavior.

- [ ] **Step 2: Run the new proxy tests and verify RED**

Run:

```bash
go test ./internal/proxy -run 'TestHTTPProxy_(InjectsGOWAUIBootstrap|DoesNotInjectGOWAUIBootstrap|JSONResponseRemainsUnchanged)' -count=1
```

Expected: the valid-cookie HTML test fails because the response does not contain `gowa-ui.connection.v1`; unchanged-path tests pass only after their assertions are correctly targeted.

- [ ] **Step 3: Wire the eligibility check into response rewriting**

Modify `HTTPProxy.ServeHTTP` and `modifyProxyResponse` so the response callback receives the request cookie header, original request URL, request scheme/host, resolved instance, and `p.magicAuth`.

Apply this sequence in `modifyProxyResponse`:

1. Retain the existing `Location` and `Set-Cookie` rewrite behavior.
2. Retain the current JSON branch unchanged.
3. For `text/html` only, return immediately unless all conditions hold:

```go
magicAuth != nil &&
magicAuth.HasValidCookie(requestCookieHeader, instanceKey, time.Now()) &&
GetInstanceBasicAuthCredentials(instance) returns ok
```

4. Read and close `resp.Body`; if reading fails, return the read error so the existing reverse-proxy error behavior applies.
5. Derive the browser-visible manager origin from the incoming request URL (`scheme://host`), not from the upstream target URL. Respect the request scheme already used elsewhere for manager requests.
6. Call `InjectGOWAUILocalStorageBootstrap`. If it returns the original body because no safe insertion point exists, still restore the original bytes and correct `Content-Length`.
7. Replace `resp.Body`, `resp.ContentLength`, and `Content-Length` with the final byte slice.

Do not change request header injection, WebSocket code, token validation, token stripping, or cookie lifetime.

- [ ] **Step 4: Run focused proxy tests and verify GREEN**

Run:

```bash
go test ./internal/proxy -run 'TestHTTPProxy_(InjectsGOWAUIBootstrap|DoesNotInjectGOWAUIBootstrap|JSONResponseRemainsUnchanged|InstanceAuthInjectedWithValidCookie|NoAuthInjectionWithoutCookie)' -count=1
```

Expected: PASS. The valid magic-cookie HTML gets both localStorage entries; all ordinary/invalid/no-credential and JSON paths remain unchanged.

- [ ] **Step 5: Run proxy package regression tests**

Run:

```bash
go test ./internal/proxy -count=1
```

Expected: PASS, including existing redirect rewriting, cookies, JSON rewriting, binary streaming, authorization precedence, and WebSocket-related helper tests.

- [ ] **Step 6: Commit the proxy integration**

```bash
git add internal/proxy/http.go internal/proxy/http_test.go
git commit -m "feat(proxy): initialize gowa-ui admin connection"
```

## Task 3: Verify HTTP API Magic-Link Integration and Repository Regression

**Files:**
- Modify only if a gap is discovered: `internal/httpapi/proxy_test.go`
- No production-file change is expected.

**Interfaces:**
- Consumes: existing `proxyRouteDeps`, `newProxyRouteServer`, `MagicAuthService`, and proxy behavior delivered by Tasks 1–2.
- Produces: regression coverage proving route-level valid magic links retain their redirect/cookie behavior while the redirected HTML request is eligible for the new bootstrap.

- [ ] **Step 1: Write a failing route-level regression test**

Add a test that obtains a valid token, requests `/app/mykey/?autologin={token}` with a cookie jar or follows the `Set-Cookie` header manually, follows the clean redirect, and asserts all of the following on the proxied HTML response:

```go
strings.Contains(body, "gowa-ui.connection.v1")
strings.Contains(body, "gowa-ui.device.v1")
strings.Contains(body, `/app/mykey`)
!strings.Contains(finalURL, "autologin")
```

Keep the existing tests for `302`, cleared query parameter, cookie name/path/HttpOnly attributes, invalid token `401`, and no-token proxy route unchanged.

- [ ] **Step 2: Run the route-level test and verify RED or identify existing coverage**

Run:

```bash
go test ./internal/httpapi -run TestProxyRoutes_AutologinInitializesGOWAUIConnection -count=1
```

Expected before implementation of Tasks 1–2: FAIL because no bootstrap exists. If executing after Tasks 1–2, it should pass immediately and the test still documents the full route-to-browser contract.

- [ ] **Step 3: Implement only required fixture support**

If the route fixture root does not return HTML, update only the test upstream fixture to return `text/html` with a module script for this test. Do not alter application routes. Ensure the instance fixture has `basicAuthConfig("admin", "secret")` and is running.

- [ ] **Step 4: Run route-level regression tests**

Run:

```bash
go test ./internal/httpapi -run 'TestProxyRoutes_(Autologin|NormalProxy)' -count=1
```

Expected: PASS. Verify the existing autologin redirect/cookie tests still behave identically and the new test confirms browser bootstrap after redirect.

- [ ] **Step 5: Run full verification**

Run:

```bash
go test ./...
```

Then run the repository's currently configured checks where available:

```bash
bun run build:tsc
bun run build:production
bun test
```

Expected: all commands exit successfully. If a Bun check does not represent the active Go backend or fails for an unrelated baseline condition, record the exact command output before making any unrelated change.

- [ ] **Step 6: Commit the route-level regression coverage**

```bash
git add internal/httpapi/proxy_test.go
git commit -m "test(proxy): cover gowa-ui magic-link bootstrap"
```

## Plan Self-Review

- **Spec coverage:** Task 1 covers safe localStorage state construction, credentials, escaping, and HTML placement. Task 2 covers valid-cookie eligibility, manager-origin base URL, response-size/header updates, unchanged non-eligible paths, JSON preservation, and backward compatibility. Task 3 covers the magic-link redirect-to-HTML integration and full regressions.
- **Placeholder scan:** No TBD/TODO/future-work steps are present; each test and implementation step identifies files, functions, commands, and expected outcomes.
- **Type consistency:** `GetInstanceBasicAuthCredentials`, `InjectGOWAUILocalStorageBootstrap`, and `isHTMLContentType` are defined in Task 1 and consumed with identical names in Task 2. Task 3 depends only on the existing HTTP API fixtures plus completed Task 2 behavior.
