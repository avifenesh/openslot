package googleoauth

import "testing"

func TestDesktopAllowed(t *testing.T) {
	for _, tc := range []struct {
		base, bind, port string
		want             bool
	}{
		{"http://127.0.0.1:8788", "127.0.0.1", "8788", true},
		{"http://localhost:8788", "127.0.0.1", "8788", true},
		{"http://[::1]:8788", "::1", "8788", true},
		{"https://schedule.example.com", "127.0.0.1", "8788", false},
		{"http://127.0.0.1:8788", "0.0.0.0", "8788", false},
		{"http://127.0.0.1:8788", "", "8788", false},
		{"http://127.0.0.1:8788", "127.0.0.1", "8888", false},
		{"http://127.0.0.1.evil.example:8788", "127.0.0.1", "8788", false},
		{"http://127.0.0.1:8788/path", "127.0.0.1", "8788", false},
		{"http://user@127.0.0.1:8788", "127.0.0.1", "8788", false},
	} {
		t.Run(tc.base+"/"+tc.bind+"/"+tc.port, func(t *testing.T) {
			if got := DesktopAllowed(tc.base, tc.bind, tc.port); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
