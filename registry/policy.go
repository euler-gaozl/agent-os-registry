package registry

import "strings"

// Authorize is this service's inbound policy. Peer authentication is supplied by the SDK.
func Authorize(caller, method, path string) bool {
	if path == "/readyz" || path == "/metrics" {
		return method == "GET" && (caller == "registry" || caller == "control")
	}
	get := method == "GET"
	if caller == "control" {
		return !strings.HasPrefix(path, "/v1/credentials/resolve")
	}
	if caller == "futures" {
		return get && (path == "/v1/status" || path == "/v1/published/futures")
	}
	if caller == "runtime" && get && strings.HasPrefix(path, "/v1/published/") {
		return true
	}
	if caller == "runtime" || caller == "worker" || caller == "knowledge" || caller == "indexer" || caller == "toolhub" || caller == "telemetry" {
		return (get && strings.HasPrefix(path, "/v1/resources/")) || (method == "POST" && path == "/v1/credentials/resolve")
	}
	return false
}
