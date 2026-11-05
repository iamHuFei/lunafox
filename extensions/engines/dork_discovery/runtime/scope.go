package runtime

import "strings"

// HostInScope mirrors the Server-side asset scope semantics
// (server/internal/modules/snapshot/domain/target_match.go) so engine-side
// pre-filtering never widens what ingestion would accept: a host is in scope
// when it equals the target domain or is any depth of subdomain of it.
func HostInScope(host, target string) bool {
	normalizedHost := strings.ToLower(strings.TrimSuffix(host, "."))
	normalizedTarget := strings.ToLower(strings.TrimSuffix(target, "."))
	if normalizedHost == "" || normalizedTarget == "" {
		return false
	}
	return normalizedHost == normalizedTarget || strings.HasSuffix(normalizedHost, "."+normalizedTarget)
}
