package appshutdown

import "strings"

var requests = make(chan string, 1)

// Request asks the running server to stop through its normal shutdown path.
// Only the first pending request is retained.
func Request(reason string) bool {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "requested"
	}
	select {
	case requests <- reason:
		return true
	default:
		return false
	}
}

// Requests exposes graceful shutdown requests to the server lifecycle.
func Requests() <-chan string {
	return requests
}
