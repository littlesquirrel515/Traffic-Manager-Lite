package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
)

func TestFailedOnlineProbePreservesFreshSnapshot(t *testing.T) {
	a, h := testAPI(t)
	now := storage.Stamp(time.Now())
	a.Store.DB.Exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'test','localhost',?,?)", now, now)
	a.Store.DB.Exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,capabilities_json,created_at,updated_at) VALUES(1,1,'test','singbox','localhost:1',?,?,?)", `[{"metric":"online_sessions","status":"unavailable"}]`, now, now)
	if e := a.Store.SaveOnline(context.Background(), 1, []core.OnlineRecord{{UserKey: "alice", Count: 2, Kind: "session", CollectedAt: time.Now()}}); e != nil {
		t.Fatal(e)
	}
	r := call(t, h, "GET", "/api/v1/online", nil, true)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":"online"`) || !strings.Contains(r.Body.String(), `"count":2`) {
		t.Fatal(r.Body.String())
	}
	a.Store.DB.Exec("UPDATE online_snapshots SET updated_at=?", storage.Stamp(time.Now().Add(-time.Hour)))
	r = call(t, h, "GET", "/api/v1/online", nil, true)
	if !strings.Contains(r.Body.String(), `"status":"stale"`) {
		t.Fatal(r.Body.String())
	}
}
