package qbutil

import "testing"

func TestIsDefaultLocalURL(t *testing.T) {
	cases := []string{
		"http://localhost:8080",
		"http://localhost:8080/",
		"http://127.0.0.1:8080",
		"http://localhost:7603",
		"http://127.0.0.1:7603",
	}

	for _, raw := range cases {
		if !IsManagedLocalURL(raw) {
			t.Fatalf("expected %q to be treated as default local URL", raw)
		}
	}
}

func TestManagedBinaryMissingForManagedMode(t *testing.T) {
	cfg := Config{
		URL:  LegacyQBURL,
		Mode: ModeManaged,
	}

	if !ManagedBinaryMissing(cfg, t.TempDir()) {
		t.Fatal("expected missing managed binary to be detected for implicit default URL")
	}
}

func TestManagedBinaryMissingSkipsExternalMode(t *testing.T) {
	cfg := Config{
		URL:  "http://192.168.1.5:8080",
		Mode: ModeExternal,
	}

	if ManagedBinaryMissing(cfg, t.TempDir()) {
		t.Fatal("explicit QB URL should not be treated as missing managed binary")
	}
}

func TestMissingExternalURL(t *testing.T) {
	cfg := Config{Mode: ModeExternal}
	if !MissingExternalURL(cfg) {
		t.Fatal("expected empty external config to be flagged")
	}

	cfg.URL = "http://qb.example.com"
	if MissingExternalURL(cfg) {
		t.Fatal("expected external URL to satisfy validation")
	}
}
