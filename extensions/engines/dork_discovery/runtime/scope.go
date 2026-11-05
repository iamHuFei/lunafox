package runtime

import (
	"net"
	"strings"
)

// HostInScope mirrors the Server-side asset scope semantics
// (server/internal/modules/snapshot/domain/target_match.go) so engine-side
// pre-filtering never widens what ingestion would accept. A domain target
// admits itself and any depth of subdomain; an IP target admits only its own
// address (any port already stripped by the caller).
func HostInScope(host, target string) bool {
	normalizedHost := strings.ToLower(strings.TrimSuffix(host, "."))
	normalizedTarget := strings.ToLower(strings.TrimSuffix(target, "."))
	if normalizedHost == "" || normalizedTarget == "" {
		return false
	}
	if net.ParseIP(normalizedTarget) != nil {
		return normalizedHost == normalizedTarget
	}
	return normalizedHost == normalizedTarget || strings.HasSuffix(normalizedHost, "."+normalizedTarget)
}

// IsIPTarget reports whether the target value is a bare IP address.
func IsIPTarget(target string) bool {
	return net.ParseIP(strings.TrimSpace(target)) != nil
}
