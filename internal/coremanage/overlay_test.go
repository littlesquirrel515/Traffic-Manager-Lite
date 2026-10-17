package coremanage

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func TestOwnedOverlayPreservesExternalUsersAndIsIdempotent(t *testing.T) {
	cases := map[string]string{"xray": `{"routing":{"domainStrategy":"AsIs"},"inbounds":[{"tag":"v","port":10443,"protocol":"vless","settings":{"clients":[{"email":"A","id":"00000000-0000-4000-8000-000000000001"},{"email":"B","id":"00000000-0000-4000-8000-000000000002"}]}}]}`, "singbox": `{"inbounds":[{"tag":"v","type":"vless","listen_port":10443,"users":[{"name":"A","uuid":"00000000-0000-4000-8000-000000000001"},{"name":"B","uuid":"00000000-0000-4000-8000-000000000002"}]}]}`, "hysteria2": "listen: :443\nauth:\n  type: userpass\n  userpass:\n    a: external-a\n    b: external-b\n"}
	for kind, base := range cases {
		t.Run(kind, func(t *testing.T) {
			tag := "v"
			if kind == "hysteria2" {
				tag = "hysteria2"
			}
			overlay := []ManagedUser{{Inbound: tag, User: User{Name: "c", UUID: "00000000-0000-4000-8000-000000000003", Password: "managed-c"}, Enabled: true}, {Inbound: tag, User: User{Name: "d", UUID: "00000000-0000-4000-8000-000000000004", Password: "managed-d"}, Enabled: true}}
			b, e := MergeOverlay(kind, []byte(base), overlay)
			if e != nil {
				t.Fatal(e)
			}
			again, e := MergeOverlay(kind, b, overlay)
			if e != nil || string(b) != string(again) {
				t.Fatal("not idempotent", e)
			}
			view, e := publicFor(kind, b)
			if e != nil || view[0]["user_count"] != 4 {
				t.Fatal(view, e)
			}
			overlay[0].User.UUID = "00000000-0000-4000-8000-000000000099"
			overlay[0].User.Password = "conflicting"
			if _, e = MergeOverlay(kind, b, overlay); e == nil {
				t.Fatal("silently replaced conflicting external authentication")
			}
			// A runtime restart doesn't destroy Desired State: applying it to a fresh base restores C/D.
			restored, e := MergeOverlay(kind, []byte(base), []ManagedUser{{Inbound: tag, User: User{Name: "c", UUID: "00000000-0000-4000-8000-000000000003", Password: "managed-c"}, Enabled: true}})
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(string(restored), "c") {
				t.Fatal("not restored")
			}
			if kind == "xray" {
				var m map[string]any
				json.Unmarshal(restored, &m)
				if m["routing"] == nil {
					t.Fatal("base network settings overwritten")
				}
			} else if kind == "hysteria2" {
				var m map[string]any
				yaml.Unmarshal(restored, &m)
				if m["listen"] != ":443" {
					t.Fatal("listener changed")
				}
			}
		})
	}
}
