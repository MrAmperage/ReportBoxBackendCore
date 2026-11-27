package static

import "testing"

func TestValidHost(t *testing.T) {
	for _, tc := range []struct {
		bound, authority string
		want             bool
	}{
		{"127.0.0.1:8080", "127.0.0.1:8080", true},
		{"127.0.0.1:8080", "LOCALHOST:8080", true},
		{"127.0.0.1:8080", "evil.example:8080", false},
		{"127.0.0.1:8080", "127.0.0.2:8080", false},
		{"127.0.0.1:8080", "localhost:80", false},
		{"127.0.0.1:8080", "localhost", false},
		{"127.0.0.1:80", "localhost", true},
		{"[::1]:8080", "[::1]:8080", true},
		{"[::1]:80", "[::1]", true},
		{"[::1]:8080", "::1:8080", false},
		{"0.0.0.0:8080", "frontend.example", true},
		{"invalid", "localhost", false},
	} {
		if got := validHost(tc.bound, tc.authority); got != tc.want {
			t.Errorf("validHost(%q, %q)=%v want %v", tc.bound, tc.authority, got, tc.want)
		}
	}
}
