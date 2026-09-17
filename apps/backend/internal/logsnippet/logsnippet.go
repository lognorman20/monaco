package logsnippet

const maxBytes = 512

// Body returns a log-safe truncated copy of an HTTP response body.
func Body(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	s := string(body)
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "…"
}
