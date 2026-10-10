package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/storage"
)

func TestNoOverlapOtherInstanceAndTimeoutPersistence(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int64
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if r.URL.Path == "/traffic" {
			w.Write([]byte(`{"alice":{"tx":0,"rx":0}}`))
		} else {
			w.Write([]byte(`{"alice":1}`))
		}
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer fast.Close()
	store, e := storage.Open(filepath.Join(t.TempDir(), "traffic.db"), "Asia/Shanghai")
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	now := storage.Stamp(time.Now())
	store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost',?,?)", now, now)
	for id, url := range map[int]string{1: slow.URL, 2: fast.URL} {
		if _, e = store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(?,1,'test','hysteria2',?,?,?)", id, url, now, now); e != nil {
			t.Fatal(e)
		}
	}
	ctx := context.Background()
	s := New(ctx, store, config.Config{Timeout: 100 * time.Millisecond, AllowedTargets: []string{"127.0.0.1"}})
	done := make(chan error, 1)
	go func() { done <- s.Collect(ctx, 1) }()
	<-started
	if e = s.Collect(ctx, 1); e != ErrBusy {
		t.Fatalf("overlap %v", e)
	}
	if e = s.Collect(ctx, 2); e != nil {
		t.Fatalf("other instance blocked %v", e)
	}
	if e = <-done; e == nil {
		t.Fatal("timeout ignored")
	}
	if calls.Load() != 3 {
		t.Fatal("expected independent traffic/online/streams requests without overlapping collection")
	}
	var last string
	if e = store.DB.QueryRow("SELECT last_error FROM instances WHERE id=1").Scan(&last); e != nil || last == "" {
		t.Fatal("timeout not recorded")
	}
	close(release)
}
