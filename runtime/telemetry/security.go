package telemetry

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func localHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "http" && u.Host == r.Host
}
func controlToken(root string) (string, error) {
	dir := filepath.Join(root, ".reticle")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "control-token")
	if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) == 64 {
		return strings.TrimSpace(string(b)), nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("control token unavailable: %w", err)
	}
	_, err = f.WriteString(token)
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return token, closeErr
}
func protected(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !localHost(r.Host) || !sameOrigin(r) {
			http.Error(w, "Forbidden origin", 403)
			return
		}
		supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if c, err := r.Cookie("reticle_control"); err == nil && supplied == "" {
			supplied = c.Value
		}
		if r.URL.Path == "/login" && r.Method == "POST" {
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			supplied = r.FormValue("token")
		}
		if len(supplied) != len(token) || subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
			if r.URL.Path == "/" && r.Method == "GET" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(401)
				fmt.Fprint(w, `<!doctype html><title>Connect to Reticle</title><h1>Connect to Reticle</h1><p>Enter the access code from your local .reticle/control-token file. Studio connects automatically.</p><form action="/login" method="post"><input type="password" name="token" autocomplete="off" required><button>Connect</button></form>`)
				return
			}
			http.Error(w, "Authentication required", 401)
			return
		}
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "reticle_control", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", 303)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SafePath rejects absolute paths, traversal, alternate separators and symlinks.
// Intended for server-owned staging/session paths, never caller-chosen roots.
func SafePath(root, relative string) (string, error) {
	if relative == "" || strings.HasPrefix(relative, "/") || filepath.IsAbs(relative) || strings.ContainsAny(relative, "\\:") {
		return "", fmt.Errorf("invalid relative path")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(base, clean)
	for current := target; current != base; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink path denied")
		}
		if err == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil || resolved != current {
				return "", fmt.Errorf("aliased path denied")
			}
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	canonical, err := filepath.EvalSymlinks(base)
	if err == nil && canonical != base {
		return "", fmt.Errorf("aliased root denied")
	}
	return target, nil
}
