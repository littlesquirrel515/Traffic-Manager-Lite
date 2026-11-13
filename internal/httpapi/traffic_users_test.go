package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"traffic-manager-lite/internal/subscription"
)

func TestTrafficUsersSearchesSubscriberAndKeepsOriginalIdentity(t *testing.T) {
	a, h := testAPI(t)
	sub, e := subscription.SaveSubscriber(context.Background(), a.Store, subscription.Subscriber{Name: "运营用户", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		"INSERT INTO servers(id,name,address,created_at,updated_at) VALUES(1,'fixture','display','now','now')",
		"INSERT INTO instances(id,server_id,name,core_type,api_endpoint,created_at,updated_at) VALUES(1,1,'fixture','xray','localhost:1','now','now')",
		"INSERT INTO users(id,display_name,created_at,updated_at) VALUES(91,'tml-node-fixture','now','now')",
		"INSERT INTO identities(user_id,instance_id,inbound_tag,core_user_key,scope) VALUES(91,1,'','tml-node-fixture','user')",
	} {
		if _, e = a.Store.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = a.Store.DB.Exec("INSERT INTO subscriber_usage_sources(subscriber_id,instance_id,inbound_id,client_ref,created_at) VALUES(?,1,'','tml-node-fixture','now')", sub); e != nil {
		t.Fatal(e)
	}
	for _, search := range []string{"%E8%BF%90%E8%90%A5%E7%94%A8%E6%88%B7", "tml-node-fixture"} {
		r := call(t, h, "GET", "/api/v1/users?search="+search, nil, true)
		var rows []map[string]any
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &rows) != nil || len(rows) != 1 {
			t.Fatal(r.Code, r.Body.String())
		}
		if rows[0]["operator_name"] != "运营用户" || rows[0]["display_name"] != "tml-node-fixture" {
			t.Fatal("identity was overwritten")
		}
		if strings.Contains(r.Body.String(), "token_sealed") {
			t.Fatal("credential leak")
		}
	}
	var name string
	a.Store.DB.QueryRow("SELECT display_name FROM users WHERE id=91").Scan(&name)
	if name != "tml-node-fixture" {
		t.Fatal("name projection wrote to the database")
	}
}
