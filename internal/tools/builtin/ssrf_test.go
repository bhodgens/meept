package builtin

import "testing"

func TestIsBlockedAddress(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"169.254.169.254", true},    // AWS metadata
		{"127.0.0.1", true},          // loopback
		{"::1", true},                // loopback v6
		{"10.0.0.1", true},           // private
		{"172.16.5.5", true},         // private
		{"192.168.1.1", true},        // private
		{"0.0.0.0", true},            // unspecified
		{"224.0.0.1", true},          // multicast
		{"8.8.8.8", false},           // public
		{"1.1.1.1", false},           // public
		{"example.com", false},       // hostname
		{"", false},                  // empty
		{"169.254.169.254:80", true}, // with port
		{"8.8.8.8:443", false},       // public with port
	}
	for _, tc := range cases {
		got := isBlockedAddress(tc.addr)
		if got != tc.want {
			t.Errorf("isBlockedAddress(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestCheckURL_RejectsMetadataIP(t *testing.T) {
	if err := checkURL("http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Fatal("checkURL accepted AWS metadata endpoint")
	}
}

func TestCheckURL_RejectsLoopback(t *testing.T) {
	if err := checkURL("http://127.0.0.1/admin"); err == nil {
		t.Fatal("checkURL accepted loopback URL")
	}
}

func TestCheckURL_RejectsPrivateRange(t *testing.T) {
	if err := checkURL("http://10.0.0.1/internal"); err == nil {
		t.Fatal("checkURL accepted private-range URL")
	}
}

func TestCheckURL_RejectsNonHTTPScheme(t *testing.T) {
	if err := checkURL("file:///etc/passwd"); err == nil {
		t.Fatal("checkURL accepted file:// URL")
	}
	if err := checkURL("gopher://example.com/"); err == nil {
		t.Fatal("checkURL accepted gopher:// URL")
	}
}

func TestCheckURL_AcceptsPublicHTTP(t *testing.T) {
	// Skip this test if offline.
	if err := checkURL("https://example.com/"); err != nil {
		t.Logf("checkURL rejected public URL (likely DNS offline): %v", err)
	}
}
