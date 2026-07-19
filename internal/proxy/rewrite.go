package proxy

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
)

// hopByHopHeaders are the per-hop headers defined by RFC 7230 that must
// not be forwarded by a proxy. The Host header is also removed because
// the proxy sets its own Host when dialing the upstream.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// PrepareForwardHeaders returns a NEW http.Header derived from
// originalHeaders, suitable for forwarding to the upstream instance.
// The original header map is never mutated.
//
// The returned headers:
//   - include X-Forwarded-For (existing value or "localhost"),
//     X-Forwarded-Proto ("http"), and X-Forwarded-Host (existing Host
//     or "localhost");
//   - have the Host header and all hop-by-hop headers removed;
//   - have an instance Basic Auth Authorization header injected when no
//     Authorization is present, a MagicAuthService is provided, and the
//     request carries a valid magic admin cookie for the instance.
func PrepareForwardHeaders(originalHeaders http.Header, instance instances.Instance, magicAuth *auth.MagicAuthService, requestURL *url.URL) http.Header {
	out := originalHeaders.Clone()

	// X-Forwarded-* headers.
	if existing := out.Get("X-Forwarded-For"); existing == "" {
		out.Set("X-Forwarded-For", "localhost")
	}
	out.Set("X-Forwarded-Proto", "http")
	if host := out.Get("Host"); host != "" {
		out.Set("X-Forwarded-Host", host)
	} else {
		out.Set("X-Forwarded-Host", "localhost")
	}

	// Remove Host and hop-by-hop headers.
	out.Del("Host")
	for _, h := range hopByHopHeaders {
		out.Del(h)
	}

	// Instance auth injection: only when no Authorization is already
	// present and a valid magic cookie exists for this instance.
	if out.Get("Authorization") == "" && magicAuth != nil && instance.Key != "" {
		cookieHeader := out.Get("Cookie")
		if magicAuth.HasValidCookie(cookieHeader, instance.Key, time.Now()) {
			if authHeader := GetInstanceBasicAuthHeader(instance); authHeader != "" {
				out.Set("Authorization", authHeader)
			}
		}
	}

	return out
}

// GetInstanceBasicAuthHeader returns the "Basic <base64(user:pass)>"
// header value for the first basicAuth entry in the instance config, or
// an empty string when no basic auth is configured or the config is
// invalid. This mirrors the Bun getFirstInstanceBasicAuthHeader.
func GetInstanceBasicAuthHeader(instance instances.Instance) string {
	if instance.Config == "" {
		return ""
	}
	var cfg struct {
		Flags struct {
			BasicAuth []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"basicAuth"`
		} `json:"flags"`
	}
	if err := json.Unmarshal([]byte(instance.Config), &cfg); err != nil {
		return ""
	}
	if len(cfg.Flags.BasicAuth) == 0 {
		return ""
	}
	first := cfg.Flags.BasicAuth[0]
	if first.Username == "" || first.Password == "" {
		return ""
	}
	creds := first.Username + ":" + first.Password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

// StripAbsoluteURLs rewrites JSON response bodies by converting absolute
// http(s) URLs into relative paths (pathname + search + hash). It walks
// objects, arrays, and nested structures recursively. Non-JSON input is
// returned unchanged. This mirrors the Bun stripAbsoluteUrls behaviour.
func StripAbsoluteURLs(data []byte) []byte {
	var node any
	if err := json.Unmarshal(data, &node); err != nil {
		return data
	}
	rewritten := stripURLs(node)
	out, err := json.Marshal(rewritten)
	if err != nil {
		return data
	}
	return out
}

// stripURLs recursively walks parsed JSON values and rewrites absolute
// http(s) URL strings into relative paths.
func stripURLs(node any) any {
	switch v := node.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, value := range v {
			result[key] = stripURLs(value)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, value := range v {
			result[i] = stripURLs(value)
		}
		return result
	case string:
		return rewriteURLString(v)
	default:
		return node
	}
}

// rewriteURLString converts an absolute http(s) URL to a relative path,
// preserving non-URL strings unchanged.
func rewriteURLString(s string) string {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	// JavaScript's URL constructor rejects URLs without a host (e.g.
	// "http://"). Preserve such strings unchanged to match Bun.
	if u.Host == "" {
		return s
	}
	// url.Pathname is always at least "/" for a valid absolute URL.
	relative := u.Path
	if relative == "" {
		relative = "/"
	}
	if u.RawQuery != "" {
		relative += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		relative += "#" + u.Fragment
	}
	return relative
}

// IsBinaryContent reports whether the given Content-Type indicates
// binary content that should be passed through without URL rewriting.
// It mirrors the Bun isBinaryContent behaviour.
func IsBinaryContent(contentType string) bool {
	binaryTypes := []string{
		"image/",
		"video/",
		"audio/",
		"application/pdf",
		"application/zip",
		"application/octet-stream",
		"font/",
		"application/font",
	}
	for _, t := range binaryTypes {
		if strings.Contains(contentType, t) {
			return true
		}
	}
	return false
}

// RewriteRedirectLocation rewrites a redirect Location header so that
// redirects pointing at the upstream's localhost URL stay within the
// proxy prefix. A Location of the form "http://localhost:{port}/path"
// is rewritten to "/app/{instanceKey}/path". A bare path that is not
// already under the proxy prefix is likewise prefixed. Locations that
// are already prefixed, or that point to an external host, are returned
// unchanged.
func RewriteRedirectLocation(location, instanceKey, requestHost string) string {
	if location == "" {
		return location
	}

	prefix := "/" + ProxyPrefix + "/" + instanceKey

	// Absolute localhost URL: strip scheme + host, keep path + query.
	if strings.HasPrefix(location, "http://localhost:") {
		u, err := url.Parse(location)
		if err != nil {
			return location
		}
		relative := u.Path
		if u.RawQuery != "" {
			relative += "?" + u.RawQuery
		}
		if u.RawFragment != "" {
			relative += "#" + u.RawFragment
		}
		if relative == "" {
			relative = "/"
		}
		return prefix + relative
	}

	// Bare absolute path: prefix unless already under the proxy prefix.
	if strings.HasPrefix(location, "/") && !strings.HasPrefix(location, prefix) {
		return prefix + location
	}

	return location
}

// RewriteSetCookie rewrites a Set-Cookie response header so that cookies
// set by the upstream instance are scoped under the proxy prefix
// (/app/{instanceKey}) instead of leaking to other paths or instances.
// The Domain attribute is removed so the cookie is host-only (bound to
// the manager's host as seen by the browser). The cookie name/value and
// other attributes (Secure, HttpOnly, SameSite, Max-Age, Expires) are
// preserved. An empty input returns an empty string.
func RewriteSetCookie(setCookie, instanceKey, requestHost string) string {
	if setCookie == "" {
		return setCookie
	}
	prefix := "/" + ProxyPrefix + "/" + instanceKey

	parts := strings.Split(setCookie, ";")
	var kept []string
	hasPath := false
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		if i == 0 {
			// The first part is the cookie name=value; keep verbatim.
			kept = append(kept, part)
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "path="):
			hasPath = true
			original := strings.TrimSpace(trimmed[len("path="):])
			rewritten := prefix
			if original != "" && original != "/" {
				rewritten = prefix + original
			}
			kept = append(kept, " Path="+rewritten)
		case strings.HasPrefix(lower, "domain="):
			// Drop the Domain attribute so the cookie is host-only.
			continue
		default:
			kept = append(kept, part)
		}
	}
	if !hasPath {
		kept = append(kept, " Path="+prefix)
	}
	return strings.Join(kept, ";")
}
