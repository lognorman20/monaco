package config

import (
	"net"
	"net/url"
	"os"
	"strings"
)

const envFakerEnabled = "FAKER_ENABLED"

// FakerEnabled reports whether POST /v1/dev/faker is enabled (#153). Default off.
// Only the seed endpoint is gated: faker skip filters in workers and reads are always on.
func FakerEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envFakerEnabled))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// IsLocalDatabaseURL mirrors scripts/assert-local-database-url.sh: postgres on a loopback
// host and never hosted Supabase. Used to keep faker seeding off shared/prod databases.
func IsLocalDatabaseURL(databaseURL string) bool {
	raw := strings.TrimSpace(databaseURL)
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "supabase.co") || strings.Contains(lower, "supabase.com") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return false
	}
	return IsLoopbackHost(u.Hostname())
}

// IsLoopbackHost reports whether host is localhost or a loopback IP.
func IsLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
