// Package proxy — HTTP reverse proxy. http.go implements the streaming
// reverse proxy that forwards browser requests mounted under
// /app/{instanceKey}/... to the corresponding GOWA instance backend
// (http://127.0.0.1:{port}{path}).
//
// The proxy builds on the pure utilities in target.go (target resolution)
// and rewrite.go (header preparation, response rewriting). It uses the
// standard library httputil.ReverseProxy so that streaming, chunked
// transfer encoding, and connection reuse are handled correctly.
//
// Design notes:
//   - No global response timeout is set on the transport or the proxy.
//     Long-running streams (e.g. event streams) must not be killed by a
//     fixed deadline. Per-request cancellation is driven by the request
//     context instead.
//   - FlushInterval is set to -1 so that chunked/streaming responses are
//     flushed to the client immediately rather than buffered.
//   - Explicit client Authorization headers always win over magic-cookie
//     instance Basic Auth injection (handled in PrepareForwardHeaders).
//   - JSON response bodies are fully buffered only to strip absolute URLs;
//     binary and other content types stream through unchanged.
package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"

	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
)

// HTTPProxy is a streaming reverse proxy that forwards requests to GOWA
// instances resolved by the TargetResolver. It is safe for concurrent use.
type HTTPProxy struct {
	resolver  *TargetResolver
	magicAuth *auth.MagicAuthService
	transport http.RoundTripper
}

// NewHTTPProxy creates an HTTPProxy. If transport is nil a default
// http.Transport is used with a 5s dial timeout, a 30s response-header
// timeout, and a 90s idle-conn timeout — but no overall Timeout, so that
// long streams are not interrupted.
func NewHTTPProxy(resolver *TargetResolver, magicAuth *auth.MagicAuthService, transport http.RoundTripper) *HTTPProxy {
	if transport == nil {
		transport = defaultProxyTransport()
	}
	return &HTTPProxy{
		resolver:  resolver,
		magicAuth: magicAuth,
		transport: transport,
	}
}

// defaultProxyTransport returns the default transport used by the proxy.
// It deliberately omits http.Transport.Timeout (which would cap the
// entire request including the streamed body) so that long-running
// streams can complete. The ResponseHeaderTimeout bounds the time spent
// waiting for the upstream to start responding.
func defaultProxyTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}
}

// ServeHTTP resolves the target instance from the request path
// (/app/{key}/...), prepares forward headers, and streams the upstream
// response back to the client. Instance-not-found yields 404; a stopped
// or portless instance yields 503; upstream connection failures yield 502.
//
// The full original request path (including the /app/{key}/ prefix) is
// forwarded to the upstream GOWA instance. GOWA is configured with a base
// path of /app/{key}/ and expects requests at that path, not at the root.
func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	instanceKey, _ := splitProxyPath(r.URL.Path)
	if instanceKey == "" {
		writeProxyError(w, http.StatusNotFound, "Instance not found")
		return
	}

	// Carry the full original path and query string through to target
	// resolution. GOWA instances are configured with a base path of
	// /app/{key}/ and serve their routes under that prefix, so the proxy
	// must forward the complete path rather than stripping the prefix.
	requestPath := r.URL.Path
	if r.URL.RawQuery != "" {
		requestPath += "?" + r.URL.RawQuery
	}

	target, err := p.resolver.ResolveTarget(r.Context(), instanceKey, requestPath)
	if err != nil {
		switch {
		case errors.Is(err, instances.ErrNotFound):
			writeProxyError(w, http.StatusNotFound, "Instance not found")
		case isNotAvailableError(err):
			writeProxyErrorWithKey(w, http.StatusServiceUnavailable, "Instance is not running", instanceKey)
		default:
			// Invalid port / unexpected resolver error.
			writeProxyError(w, http.StatusBadGateway, "Proxy request failed")
		}
		return
	}

	instance := target.Instance
	requestHost := r.Host

	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			// Set r.Out.URL directly rather than calling r.SetURL: SetURL
			// joins the target's base path with the incoming request path,
			// which would double the path. ResolveTarget already built the
			// full upstream URL (scheme, host, path, query), so we clone it
			// verbatim. The clone prevents the transport from mutating the
			// resolver's URL.
			out := *target.URL
			r.Out.URL = &out
			r.Out.Host = target.URL.Host
			// Replace the outgoing header set entirely with the prepared
			// headers (X-Forwarded-*, hop-by-hop stripped, auth injected).
			r.Out.Header = PrepareForwardHeaders(r.In.Header, instance, p.magicAuth, r.In.URL)
		},
		ModifyResponse: func(resp *http.Response) error {
			return modifyProxyResponse(resp, instanceKey, requestHost)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// When the client disconnects (request context cancelled) there
			// is nobody to receive an error response; suppress it to avoid
			// spurious writes to a closed connection. Upstream failures
			// (connection refused, response-header timeout, etc.) do NOT
			// cancel the request context, so they fall through to a 502.
			if r.Context().Err() != nil {
				return
			}
			writeProxyError(w, http.StatusBadGateway, "Proxy request failed")
		},
		FlushInterval: -1, // flush immediately for streaming responses
		Transport:     p.transport,
	}
	rp.ServeHTTP(w, r)
}

// modifyProxyResponse rewrites upstream response headers and bodies so
// they are scoped under the proxy prefix:
//   - redirect Location headers are rewritten to /app/{key}/...;
//   - Set-Cookie paths are scoped under /app/{key} and Domain dropped;
//   - JSON response bodies have absolute http(s) URLs converted to
//     relative paths (binary content is left untouched and streams
//     through without buffering).
func modifyProxyResponse(resp *http.Response, instanceKey, requestHost string) error {
	// Rewrite redirect Location.
	if loc := resp.Header.Get("Location"); loc != "" {
		resp.Header.Set("Location", RewriteRedirectLocation(loc, instanceKey, requestHost))
	}

	// Rewrite Set-Cookie headers (path scoped, domain removed).
	if cookies := resp.Header.Values("Set-Cookie"); len(cookies) > 0 {
		rewritten := make([]string, 0, len(cookies))
		for _, c := range cookies {
			rewritten = append(rewritten, RewriteSetCookie(c, instanceKey, requestHost))
		}
		resp.Header["Set-Cookie"] = rewritten
	}

	// JSON body URL stripping. Only application/json responses are
	// buffered; binary and other content types stream through unchanged.
	ct := resp.Header.Get("Content-Type")
	if isJSONContentType(ct) {
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		stripped := StripAbsoluteURLs(body)
		resp.Body = io.NopCloser(bytes.NewReader(stripped))
		resp.ContentLength = int64(len(stripped))
		resp.Header.Set("Content-Length", strconv.Itoa(len(stripped)))
	}
	return nil
}

// splitProxyPath splits a request path of the form /app/{key}/... into
// the instance key and the sub-path forwarded to the upstream. It returns
// empty strings when the path is not under the proxy prefix.
func splitProxyPath(path string) (instanceKey, subPath string) {
	prefix := "/" + ProxyPrefix + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", ""
	}
	rest := path[len(prefix):]
	if rest == "" {
		return "", ""
	}
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		// /app/{key} with no trailing slash → forward the root path.
		return rest, "/"
	}
	return rest[:idx], rest[idx:]
}

// isNotAvailableError reports whether the resolver error indicates the
// instance exists but cannot be proxied (stopped / no port). The
// resolver wraps these in a fixed message; we match on it rather than
// exposing a sentinel error type.
func isNotAvailableError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "is not available")
}

// isJSONContentType reports whether the given Content-Type indicates a
// JSON response that should have absolute URLs stripped.
func isJSONContentType(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "application/json") || strings.Contains(ct, "+json")
}

// writeProxyError writes a JSON error response with the given status and
// message. It is used for proxy-level failures (not found, not running,
// upstream unreachable).
func writeProxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   message,
		"success": false,
	})
}

// writeProxyErrorWithKey is like writeProxyError but includes the
// instanceKey field, matching the Bun reference for the 503 response.
func writeProxyErrorWithKey(w http.ResponseWriter, status int, message, instanceKey string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":       message,
		"success":     false,
		"instanceKey": instanceKey,
	})
}
