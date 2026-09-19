package app

import "regexp"

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isUUID reports whether raw is a canonical UUID; route ids are validated before any query.
func isUUID(raw string) bool {
	return uuidPattern.MatchString(raw)
}
