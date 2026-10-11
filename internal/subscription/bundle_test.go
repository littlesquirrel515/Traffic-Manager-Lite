package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"traffic-manager-lite/internal/storage"
)

func bundleStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "subscriptions.db"), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := s.DB.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'server','localhost','old','old')")
	for i := 1; i <= 3; i++ {
		exec("INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(?,1,'instance','xray','localhost:10085','old','old')", i)
		exec("INSERT INTO users(id,display_name,created_at,updated_at) VALUES(?,'user','old','old')", i)
		n := node("vless", "ws")
		n.Profile.Name = []string{"first", "second", "unselected"}[i-1]
		b, _ := json.Marshal(n.Profile)
		exec("INSERT INTO nodes(id,instance_id,inbound_tag,user_id,protocol,discovered_json,effective_json,source_hash,updated_at) VALUES(?,?,'in',?,'vless',?,?,'hash','old')", n.Profile.Name, i, i, string(b), string(b))
	}
	return s
}

func TestBundleAcrossInstancesAndUsersPreservesExplicitOrder(t *testing.T) {
	s := bundleStore(t)
	ctx := context.Background()
	for _, format := range []string{"v2ray", "mihomo", "singbox"} {
		d := Definition{Name: "bundle", Scope: "bundle", Format: format, NodeIDs: []string{"second", "first"}, Enabled: true}
		p, e := Preview(ctx, s, d)
		if e != nil || p.Selected != 2 || p.Usable != 2 {
			t.Fatalf("preview: %+v %v", p, e)
		}
		id, token, e := Save(ctx, s, 0, d)
		if e != nil {
			t.Fatal(e)
		}
		b, _, rejected, e := Output(ctx, s, format, token)
		if e != nil || len(rejected) != 0 {
			t.Fatal(e, rejected)
		}
		if format == "v2ray" {
			b, e = base64.StdEncoding.DecodeString(string(b))
			if e != nil {
				t.Fatal(e)
			}
		}
		text := string(b)
		if strings.Index(text, "second") < 0 || strings.Index(text, "first") < strings.Index(text, "second") || strings.Contains(text, "unselected") {
			t.Fatalf("explicit node order/set lost: %s", text)
		}
		var user any
		var scope string
		if e = s.DB.QueryRow("SELECT user_id,selection_scope FROM subscriptions WHERE id=?", id).Scan(&user, &scope); e != nil || user != nil || scope != "bundle" {
			t.Fatal("bundle has wrong owner", user, scope, e)
		}
		// Editing does not rotate the token; a failed update cannot remove saved nodes.
		d.NodeIDs = []string{"first", "first"}
		if _, _, e = Save(ctx, s, id, d); e == nil {
			t.Fatal("duplicate accepted")
		}
		if _, _, _, e = Output(ctx, s, format, token); e != nil {
			t.Fatal("failed edit damaged subscription", e)
		}
		d.NodeIDs = []string{"first"}
		if _, next, e := Save(ctx, s, id, d); e != nil || next != "" {
			t.Fatal("edit rotated token", e)
		}
		if _, _, _, e = Output(ctx, s, format, token); e != nil {
			t.Fatal("token lost on edit", e)
		}
	}
}

func TestEmptyAndFilteredSubscriptionsExplainFailures(t *testing.T) {
	s := bundleStore(t)
	ctx := context.Background()
	bad := node("anytls", "tcp")
	b, _ := json.Marshal(bad.Profile)
	if _, e := s.DB.Exec("UPDATE nodes SET effective_json=? WHERE id='second'", string(b)); e != nil {
		t.Fatal(e)
	}
	d := Definition{Name: "filtered", Scope: "bundle", Format: "v2ray", NodeIDs: []string{"second"}, Enabled: true}
	p, e := Preview(ctx, s, d)
	if e != nil || p.Usable != 0 || len(p.Rejected) != 1 || !strings.Contains(p.Rejected[0].Reason, "AnyTLS") {
		t.Fatal(p, e)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), bad.Profile.Password) || strings.Contains(string(raw), bad.Profile.UUID) {
		t.Fatal("preview leaks credential")
	}
	if _, _, e := Save(ctx, s, 0, d); !errors.Is(e, ErrNoUsableNodes) {
		t.Fatal("empty enabled subscription accepted", e)
	}
	d.NodeIDs = []string{}
	if _, _, e := Save(ctx, s, 0, d); !errors.Is(e, ErrNoUsableNodes) {
		t.Fatal(e)
	}
	d.Enabled = false
	if _, token, e := Save(ctx, s, 0, d); e != nil {
		t.Fatal(e)
	} else if _, _, _, e = Output(ctx, s, "v2ray", token); e == nil {
		t.Fatal("draft public")
	}
	d.Enabled = true
	d.NodeIDs = []string{"second", "first"}
	_, token, e := Save(ctx, s, 0, d)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, r, e := Output(ctx, s, "v2ray", token); e != nil || len(r) != 1 {
		t.Fatal("partial output lost", r, e)
	}
	if _, e = s.DB.Exec("UPDATE nodes SET enabled=0 WHERE id='first'"); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = Output(ctx, s, "v2ray", token); !errors.Is(e, ErrNoUsableNodes) {
		t.Fatal("empty runtime output returned success", e)
	}
	d.Format = "singbox"
	d.NodeIDs = []string{"missing"}
	if p, e = Preview(ctx, s, d); e != nil || p.Usable != 0 || len(p.Rejected) != 1 {
		t.Fatal(p, e)
	}
}
