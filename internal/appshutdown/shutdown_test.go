package appshutdown

import (
	"testing"
	"time"
)

func TestRequestRetainsFirstPendingReason(t *testing.T) {
	drainRequests()
	t.Cleanup(drainRequests)

	if !Request("first") {
		t.Fatal("first shutdown request was not accepted")
	}
	if Request("second") {
		t.Fatal("second shutdown request should not replace a pending request")
	}

	select {
	case reason := <-Requests():
		if reason != "first" {
			t.Fatalf("shutdown reason = %q, want %q", reason, "first")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown request was not delivered")
	}
}

func drainRequests() {
	for {
		select {
		case <-Requests():
		default:
			return
		}
	}
}
