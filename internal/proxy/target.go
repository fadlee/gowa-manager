// Package proxy provides pure utility functions for resolving the
// upstream target of a proxied GOWA instance and for rewriting request
// headers and response bodies. The functions in this package perform no
// I/O of their own; they prepare data for the HTTP reverse proxy (Task 5)
// and proxy routes (Task 7) to consume.
//
// Security note (SSRF): target resolution constructs the upstream URL
// from a hardcoded scheme ("http"), a hardcoded host ("localhost"), and
// a port read from the database. No request input can influence the
// scheme, hostname, or port — only the path component of the request is
// forwarded, and it is treated purely as a path.
package proxy

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/fadlee/gowa-manager/internal/instances"
)

// ProxyPrefix is the URL prefix under which instance proxies are
// mounted. It matches the Bun reference Proxy.PREFIX ("app").
const ProxyPrefix = "app"

// Target describes a resolved upstream proxy target.
type Target struct {
	URL      *url.URL
	Instance instances.Instance
}

// TargetResolver resolves the upstream target for a given instance key
// and request path. It depends only on an instances.Repository lookup.
type TargetResolver struct {
	repo instances.Repository
}

// NewTargetResolver creates a TargetResolver backed by the given
// repository.
func NewTargetResolver(repo instances.Repository) *TargetResolver {
	return &TargetResolver{repo: repo}
}

// ResolveTarget looks up the instance by key and constructs the upstream
// target URL as http://localhost:{port}{requestPath}. It returns an
// error when the instance is not found, is not running, has no port, or
// has an invalid port. The request path is treated purely as a path:
// any scheme or host it may carry is discarded so that no request input
// can select the upstream scheme, hostname, or port.
func (r *TargetResolver) ResolveTarget(ctx context.Context, instanceKey, requestPath string) (Target, error) {
	if strings.TrimSpace(instanceKey) == "" {
		return Target{}, fmt.Errorf("proxy: instance key is required")
	}

	instance, err := r.repo.FindByKey(ctx, instanceKey)
	if err != nil {
		return Target{}, err
	}

	if !IsInstanceAvailable(instance) {
		return Target{}, fmt.Errorf("proxy: instance %q is not available", instanceKey)
	}

	port := *instance.Port
	if port < 1 || port > 65535 {
		return Target{}, fmt.Errorf("proxy: instance %q has invalid port %d", instanceKey, port)
	}

	// Build the target with a hardcoded scheme and host. The request
	// path is parsed as a reference and only its path/query/fragment
	// components are copied — never its scheme or host — so a
	// malicious path cannot redirect the upstream connection.
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", port),
	}
	if requestPath != "" {
		ref, err := url.Parse(requestPath)
		if err != nil {
			return Target{}, fmt.Errorf("proxy: invalid request path: %w", err)
		}
		target.Path = ref.Path
		target.RawQuery = ref.RawQuery
		target.RawFragment = ref.RawFragment
	}
	if target.Path == "" {
		target.Path = "/"
	}

	return Target{URL: target, Instance: instance}, nil
}

// IsInstanceAvailable reports whether an instance can be proxied: it
// must be running and have a non-nil port.
func IsInstanceAvailable(instance instances.Instance) bool {
	return instance.Status == "running" && instance.Port != nil
}
