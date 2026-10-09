package security

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginAndCSRF(t *testing.T) {
	a, e := NewAuth("admin", "test-password-123456", false)
	if e != nil {
		t.Fatal(e)
	}
	login := httptest.NewRequest("POST", "http://example.test/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"test-password-123456"}`))
	w := httptest.NewRecorder()
	a.Login(w, login)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	var out map[string]string
	json.Unmarshal(w.Body.Bytes(), &out)
	h := a.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		csrf, origin string
		expected     int
	}{{"", "", 403}, {out["csrf"], "http://evil.test", 403}, {out["csrf"], "http://example.test", 204}} {
		r := httptest.NewRequest("POST", "http://example.test/api/v1/nodes", nil)
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", tc.csrf)
		r.Header.Set("Origin", tc.origin)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.expected {
			t.Fatalf("CSRF status %d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/users", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated API accessible")
	}
}
func TestSSRFPolicy(t *testing.T) {
	p := Policy{Allowed: []string{"127.0.0.1"}}
	if _, e := p.Dial(context.Background(), "169.254.169.254:80"); e == nil {
		t.Fatal("metadata allowed")
	}
	if _, e := p.Dial(context.Background(), "192.0.2.1:80"); e == nil {
		t.Fatal("unlisted target allowed")
	}
	for _, s := range []string{"file:///etc/passwd", "http://secret@localhost", "http://localhost?clear=1"} {
		if e := ValidateEndpoint(s, true); e == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}
