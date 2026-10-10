package hysteria2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"traffic-manager-lite/internal/core"
)

func TestOfficialAPIDirectionsAndNoReset(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "secret" {
			t.Error("missing secret")
		}
		if r.URL.RawQuery != "" {
			t.Error("must not clear counters")
		}
		switch r.URL.Path {
		case "/traffic":
			w.Write([]byte(`{"alice":{"tx":300,"rx":100}}`))
		case "/online":
			w.Write([]byte(`{"alice":2}`))
		default:
			t.Error("unknown API")
		}
	}))
	defer s.Close()
	a := &Adapter{Instance: core.Instance{ID: 1, APIEndpoint: s.URL, APISecret: "secret"}, Client: s.Client()}
	rows, e := a.CollectTraffic(context.Background())
	if e != nil || len(rows) != 1 || rows[0].UploadBytes != 300 || rows[0].DownloadBytes != 100 {
		t.Fatalf("direction %v %v", rows, e)
	}
	on, e := a.CollectOnline(context.Background())
	if e != nil || on[0].Count != 2 || on[0].Kind != "device" {
		t.Fatalf("online %v %v", on, e)
	}
}
func TestResponseErrorDoesNotLeakBody(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403); w.Write([]byte("private-secret")) }))
	defer s.Close()
	a := &Adapter{Instance: core.Instance{APIEndpoint: s.URL}, Client: s.Client()}
	if _, e := a.CollectTraffic(context.Background()); e == nil || e.Error() != "Hysteria2 API status 403" {
		t.Fatalf("unsafe error %v", e)
	}
}
