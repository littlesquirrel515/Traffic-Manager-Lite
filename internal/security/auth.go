package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type attempt struct {
	Count int
	Start time.Time
}
type Auth struct {
	User, Password string
	Secure         bool
	key            []byte
	mu             sync.Mutex
	attempts       map[string]attempt
}

func NewAuth(user, password string, secure bool) (*Auth, error) {
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return nil, e
	}
	return &Auth{User: user, Password: password, Secure: secure, key: key, attempts: map[string]attempt{}}, nil
}
func (a *Auth) sign(v string) string {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(v))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func equal(a, b string) bool {
	x := sha256.Sum256([]byte(a))
	y := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func (a *Auth) session(r *http.Request) (string, bool) {
	c, e := r.Cookie("tml_session")
	if e != nil {
		return "", false
	}
	p := strings.Split(c.Value, ".")
	if len(p) != 3 {
		return "", false
	}
	expiry, e := strconv.ParseInt(p[0], 10, 64)
	if e != nil || time.Now().Unix() > expiry || !equal(p[2], a.sign(p[0]+"."+p[1])) {
		return "", false
	}
	return p[1], true
}
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	return o == "https://"+r.Host || o == "http://"+r.Host
}
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "origin forbidden", 403)
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	a.mu.Lock()
	for k, v := range a.attempts {
		if now.Sub(v.Start) > 5*time.Minute {
			delete(a.attempts, k)
		}
	}
	v := a.attempts[host]
	if v.Start.IsZero() {
		v.Start = now
	}
	v.Count++
	if len(a.attempts) > 1024 || v.Count > 10 {
		a.mu.Unlock()
		http.Error(w, "登录过于频繁，请稍后重试", 429)
		return
	}
	a.attempts[host] = v
	a.mu.Unlock()
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
		http.Error(w, "invalid login", 400)
		return
	}
	if !equal(body.Username, a.User) || !equal(body.Password, a.Password) {
		http.Error(w, "用户名或密码错误", 401)
		return
	}
	a.mu.Lock()
	delete(a.attempts, host)
	a.mu.Unlock()
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		http.Error(w, "login failed", 500)
		return
	}
	csrf := base64.RawURLEncoding.EncodeToString(b)
	payload := fmt.Sprintf("%d.%s", now.Add(12*time.Hour).Unix(), csrf)
	http.SetCookie(w, &http.Cookie{Name: "tml_session", Value: payload + "." + a.sign(payload), Path: "/", HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"csrf": csrf})
}
func (a *Auth) Session(w http.ResponseWriter, r *http.Request) {
	csrf, ok := a.session(r)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"csrf": csrf, "username": a.User})
}
func (a *Auth) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		csrf, ok := a.session(r)
		if !ok {
			if u, p, yes := r.BasicAuth(); yes && equal(u, a.User) && equal(p, a.Password) {
				next.ServeHTTP(w, r)
				return
			}
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				http.Redirect(w, r, "/login.html", 303)
			} else {
				http.Error(w, "unauthorized", 401)
			}
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && (!sameOrigin(r) || !equal(r.Header.Get("X-CSRF-Token"), csrf)) {
			http.Error(w, "CSRF verification failed", 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "tml_session", Value: "", Path: "/", HttpOnly: true, Secure: a.Secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(204)
}
