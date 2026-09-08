package telemetry

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlAuthentication(t *testing.T) {
	h := protected(strings.Repeat("a", 64), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		host, origin, token string
		want                int
	}{
		{"127.0.0.1:8080", "", "", 401},
		{"attacker.test", "", strings.Repeat("a", 64), 403},
		{"127.0.0.1:8080", "https://attacker.test", strings.Repeat("a", 64), 403},
		{"127.0.0.1:8080", "", strings.Repeat("a", 64), 204},
	} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/api/models", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("host %s: %d != %d", tc.host, w.Code, tc.want)
		}
	}
}
func TestSafePath(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"../escape", "..\\escape", "C:/escape", "/escape", ""} {
		if _, err := SafePath(root, p); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
	if _, err := SafePath(root, "safe/file.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err == nil {
		if _, err = SafePath(root, "link/file"); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
}
