// Package proxy — WebSocket bridge. websocket.go upgrades incoming
// WebSocket requests mounted under /app/{instanceKey}/... and bridges
// them to the corresponding GOWA instance's upstream WebSocket
// endpoint (ws://127.0.0.1:{port}{path}).
//
// The bridge builds on the pure target resolution in target.go and the
// header/auth utilities in rewrite.go. It uses github.com/coder/websocket
// for both the server (Accept) and client (Dial) sides.
//
// Design:
//   - One copy loop runs in each direction under a shared cancellable
//     context. When either loop exits (normal close, abnormal
//     disconnect, or cancellation) the context is cancelled and both
//     sides are closed idempotently.
//   - The upstream connection is wrapped in a WSConnection and
//     registered so that shutdown (CloseAll) and instance restart
//     (CloseAllForInstance) can tear it down.
//   - Only a safe allowlist of headers is forwarded to the upstream;
//     Sec-WebSocket-Protocol is forwarded as subprotocols.
//   - When the client supplies no Authorization header, the instance's
//     Basic Auth is injected (matching the Bun default behaviour where
//     PROXY_WS_INJECT_INSTANCE_AUTH is not "false").
//
// Message size limit: the default read limit for the copy loops is
// DefaultWSReadLimit (1 MiB). The coder/websocket library's own default
// is 32 KiB; we raise it so that typical GOWA admin payloads (which the
// Bun proxy forwarded without an explicit cap) pass through. The limit
// is configurable via NewWSBridgeWithLimit.
package proxy

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/fadlee/gowa-manager/internal/auth"
	"github.com/fadlee/gowa-manager/internal/instances"
	"github.com/google/uuid"
)

// DefaultWSReadLimit is the default maximum message size (in bytes) the
// bridge will read from either side before closing the connection with
// StatusMessageTooBig. It is set to 1 MiB to accommodate typical admin
// UI payloads while bounding memory use. Use NewWSBridgeWithLimit to
// override it.
const DefaultWSReadLimit = 1 << 20 // 1 MiB

// wsHeaderAllowlist is the set of request headers forwarded to the
// upstream WebSocket, matching the Bun allowList in websocket-utils.ts.
var wsHeaderAllowlist = []string{
	"Authorization",
	"Cookie",
	"Origin",
	"User-Agent",
	"Accept-Language",
}

// WSBridge bridges client WebSocket connections to upstream GOWA
// instance WebSocket endpoints. It is safe for concurrent use.
type WSBridge struct {
	resolver   *TargetResolver
	magicAuth  *auth.MagicAuthService
	registry   *Registry
	readLimit  int64
	injectAuth bool
}

// NewWSBridge creates a WSBridge with the default 1 MiB read limit and
// instance auth injection enabled (matching the Bun default).
func NewWSBridge(resolver *TargetResolver, magicAuth *auth.MagicAuthService, registry *Registry) *WSBridge {
	return &WSBridge{
		resolver:   resolver,
		magicAuth:  magicAuth,
		registry:   registry,
		readLimit:  DefaultWSReadLimit,
		injectAuth: true,
	}
}

// NewWSBridgeWithLimit is like NewWSBridge but allows overriding the
// per-message read limit and the instance auth injection flag. A
// readLimit <= 0 falls back to DefaultWSReadLimit.
func NewWSBridgeWithLimit(resolver *TargetResolver, magicAuth *auth.MagicAuthService, registry *Registry, readLimit int64, injectAuth bool) *WSBridge {
	if readLimit <= 0 {
		readLimit = DefaultWSReadLimit
	}
	return &WSBridge{
		resolver:   resolver,
		magicAuth:  magicAuth,
		registry:   registry,
		readLimit:  readLimit,
		injectAuth: injectAuth,
	}
}

// CloseAll closes every active upstream connection tracked by the
// bridge's registry. It is used during manager shutdown; closing the
// upstreams cascades to the client copy loops and tears them down.
func (b *WSBridge) CloseAll() {
	if b.registry != nil {
		b.registry.CloseAll()
	}
}

// ServeWS is the http.HandlerFunc that upgrades a client WebSocket and
// bridges it to the resolved upstream instance. Instance-not-found
// yields a 404 written before the upgrade; a stopped/portless instance
// yields a 503. Upstream dial failures result in a close with
// StatusInternalError after the upgrade.
func (b *WSBridge) ServeWS(w http.ResponseWriter, r *http.Request) {
	instanceKey, _ := splitProxyPath(r.URL.Path)
	if instanceKey == "" {
		writeProxyError(w, http.StatusNotFound, "Instance not found")
		return
	}

	// Carry the full original path and query string through to target
	// resolution. GOWA instances are configured with a base path of
	// /app/{key}/ and expect WebSocket requests at that path.
	requestPath := r.URL.Path
	if r.URL.RawQuery != "" {
		requestPath += "?" + r.URL.RawQuery
	}

	target, err := b.resolver.ResolveTarget(r.Context(), instanceKey, requestPath)
	if err != nil {
		switch {
		case errors.Is(err, instances.ErrNotFound):
			writeProxyError(w, http.StatusNotFound, "Instance not found")
		case isNotAvailableError(err):
			writeProxyErrorWithKey(w, http.StatusServiceUnavailable, "Instance is not running", instanceKey)
		default:
			writeProxyError(w, http.StatusBadGateway, "Proxy request failed")
		}
		return
	}

	// Forward subprotocols requested by the client.
	subprotocols := parseSubprotocols(r.Header)

	// Accept the client connection. InsecureSkipVerify is set because
	// the manager proxies arbitrary instance origins; origin checks are
	// the responsibility of the manager's auth layer, not the WS layer.
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:       subprotocols,
		InsecureSkipVerify: true,
	})
	if err != nil {
		// Accept already wrote an error response.
		return
	}
	clientConn.SetReadLimit(b.readLimit)

	// Build the upstream ws:// URL from the resolved http:// target.
	// Clone the URL so the resolver's value is not mutated (matching
	// the HTTP proxy's behaviour in http.go).
	upstreamURL := *target.URL
	upstreamURL.Scheme = "ws"

	// Prepare forwarded headers and inject instance auth when needed.
	upstreamHeaders := prepareWSHeaders(r.Header, target.Instance, b.injectAuth)

	dialCtx, dialCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer dialCancel()
	upstreamConn, _, err := websocket.Dial(dialCtx, upstreamURL.String(), &websocket.DialOptions{
		HTTPHeader:   upstreamHeaders,
		Subprotocols: subprotocols,
	})
	if err != nil {
		_ = clientConn.Close(websocket.StatusInternalError, "upstream unavailable")
		return
	}
	upstreamConn.SetReadLimit(b.readLimit)

	// Register the upstream connection so shutdown/restart can tear it
	// down. Use a unique per-client id.
	connID := instanceKey + ":" + uuid.NewString()
	wsc := &WSConnection{conn: upstreamConn, id: connID, key: instanceKey}
	if b.registry != nil {
		b.registry.Add(connID, wsc)
	}

	b.bridge(r.Context(), clientConn, wsc)

	if b.registry != nil {
		b.registry.Delete(connID)
	}
}

// bridge runs the two copy loops (client→upstream and upstream→client)
// under a shared cancellable context derived from parent. When either
// loop exits, the context is cancelled and both sides are closed
// idempotently. The upstream WSConnection is always closed via its
// idempotent Close so registry-initiated closes do not double-close.
func (b *WSBridge) bridge(parent context.Context, client *websocket.Conn, upstream *WSConnection) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	var clientCloseOnce, upstreamCloseOnce sync.Once
	closeClient := func() {
		clientCloseOnce.Do(func() { _ = client.CloseNow() })
	}
	closeUpstream := func() {
		upstreamCloseOnce.Do(func() { upstream.CloseNow() })
	}
	defer closeClient()
	defer closeUpstream()

	done := make(chan struct{}, 2)

	// client → upstream
	go func() {
		defer func() { done <- struct{}{} }()
		err := copyMessages(ctx, client, upstream.conn)
		// If the client closed cleanly, forward the close to upstream.
		if err != nil {
			code, reason := closeFromError(err)
			upstreamCloseOnce.Do(func() { _ = upstream.Close(code, reason) })
		}
		cancel()
	}()

	// upstream → client
	go func() {
		defer func() { done <- struct{}{} }()
		err := copyMessages(ctx, upstream.conn, client)
		if err != nil {
			code, reason := closeFromError(err)
			clientCloseOnce.Do(func() { _ = client.Close(code, reason) })
		}
		cancel()
	}()

	// Wait for both loops to exit before returning so the deferred
	// closes run only after the loops have stopped reading.
	<-done
	<-done
}

// copyMessages reads messages from src and writes them to dst until an
// error (close, disconnect, or context cancellation) occurs. Both text
// and binary messages are forwarded with their original type preserved.
func copyMessages(ctx context.Context, src, dst *websocket.Conn) error {
	for {
		msgType, data, err := src.Read(ctx)
		if err != nil {
			return err
		}
		if err := dst.Write(ctx, msgType, data); err != nil {
			return err
		}
	}
}

// closeFromError extracts the WebSocket close code and reason from a
// read error. When the peer performed a normal close handshake, the
// code and reason are forwarded to the other side. For abnormal
// disconnects or cancellation, StatusGoingAway is used as a safe
// default (the actual close is best-effort since the peer may be gone).
func closeFromError(err error) (websocket.StatusCode, string) {
	if err == nil {
		return websocket.StatusNormalClosure, ""
	}
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Code, ce.Reason
	}
	// Context cancellation or a raw network error: there is no close
	// frame to forward. Use StatusGoingAway as a benign default.
	return websocket.StatusGoingAway, ""
}

// parseSubprotocols extracts the WebSocket subprotocols requested by the
// client from the Sec-WebSocket-Protocol header. Each comma-separated
// value is trimmed of surrounding whitespace.
func parseSubprotocols(h http.Header) []string {
	values := h.Values("Sec-WebSocket-Protocol")
	if len(values) == 0 {
		return nil
	}
	var protos []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				protos = append(protos, p)
			}
		}
	}
	return protos
}

// prepareWSHeaders returns a new http.Header containing only the safe
// allowlisted headers from incoming, with instance Basic Auth injected
// when injectAuth is true and no Authorization header is present. The
// original header map is not mutated.
func prepareWSHeaders(incoming http.Header, instance instances.Instance, injectAuth bool) http.Header {
	out := make(http.Header, len(wsHeaderAllowlist))
	for _, h := range wsHeaderAllowlist {
		if vals := incoming.Values(h); len(vals) > 0 {
			out[h] = append(out[h], vals...)
		}
	}
	if injectAuth && out.Get("Authorization") == "" {
		if authHeader := GetInstanceBasicAuthHeader(instance); authHeader != "" {
			out.Set("Authorization", authHeader)
		}
	}
	return out
}
