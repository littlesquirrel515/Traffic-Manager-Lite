package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"testing"
)

func TestFullRendererHasUsableGroupsAndUniqueTags(t *testing.T) {
	n := node("vless", "ws")
	n.Profile.Name = "节点选择"
	input := []ResolvedClientNode{{ID: "a", Profile: n.Profile}, {ID: "b", Profile: n.Profile}}
	for _, format := range []string{"mihomo", "singbox"} {
		r, e := Render(context.Background(), format, input, RenderOptions{})
		if e != nil {
			t.Fatal(e)
		}
		var config map[string]any
		if format == "mihomo" {
			e = yaml.Unmarshal(r.Content, &config)
			groups := config["proxy-groups"].([]any)
			if len(groups) != 1 || len(config["rules"].([]any)) == 0 {
				t.Fatal("rule mode would have no selectable nodes")
			}
			proxies := config["proxies"].([]any)
			names := map[string]bool{"DIRECT": true}
			for _, v := range proxies {
				names[v.(map[string]any)["name"].(string)] = true
			}
			for _, name := range groups[0].(map[string]any)["proxies"].([]any) {
				if !names[name.(string)] {
					t.Fatal("group references missing node")
				}
			}
		} else {
			e = json.Unmarshal(r.Content, &config)
			if len(config["inbounds"].([]any)) != 1 {
				t.Fatal("missing local inbound")
			}
			seen := map[string]bool{}
			for _, v := range config["outbounds"].([]any) {
				tag := v.(map[string]any)["tag"].(string)
				if seen[tag] {
					t.Fatal("duplicate tag")
				}
				seen[tag] = true
			}
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	raw, e := Render(context.Background(), "raw", input, RenderOptions{})
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := Render(context.Background(), "base64", input, RenderOptions{})
	if e != nil {
		t.Fatal(e)
	}
	b, e := base64.StdEncoding.DecodeString(string(encoded.Content))
	if e != nil || string(b) != string(raw.Content) {
		t.Fatal("raw/base64 mismatch")
	}
}
