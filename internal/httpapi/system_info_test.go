package httpapi

import (
	"encoding/json"
	"testing"
	"traffic-manager-lite/pkg/systeminfo"
)

func TestSystemInfoAuthenticatedAndIndependent(t *testing.T) {
	a, h := testAPI(t)
	if r := call(t, h, "GET", "/api/v1/system/info", nil, false); r.Code != 401 {
		t.Fatal("metrics exposed", r.Code)
	}
	a.Store = nil
	a.Scheduler = nil
	r := call(t, h, "GET", "/api/v1/system/info", nil, true)
	var first systeminfo.Snapshot
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &first) != nil || first.Process.PID == 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = call(t, h, "GET", "/api/v1/system/info", nil, true)
	var second systeminfo.Snapshot
	json.Unmarshal(r.Body.Bytes(), &second)
	if !first.ObservedAt.Equal(second.ObservedAt) {
		t.Fatal("per-client collection bypassed cache")
	}
	if r := call(t, h, "POST", "/api/v1/system/info", nil, true); r.Code != 405 {
		t.Fatal("unexpected write route", r.Code)
	}
}
