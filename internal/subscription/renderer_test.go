package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"path/filepath"
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

func TestMihomoAutoGroupOnlyContainsDeliveredMembers(t *testing.T) {
	n := node("vless", "ws")
	n.Profile.Name = "same"
	disabled := n.Profile
	disabled.Enabled = false
	input := []ResolvedClientNode{
		{ID: "a", Profile: n.Profile, Groups: []string{"SG 优选"}},
		{ID: "b", Profile: n.Profile, Groups: []string{"SG 优选"}},
		{ID: "c", Profile: disabled, Groups: []string{"SG 优选"}},
		{ID: "d", Profile: n.Profile, Groups: []string{"其他"}},
	}
	r, err := Render(context.Background(), "mihomo", input, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = yaml.Unmarshal(r.Content, &config); err != nil {
		t.Fatal(err)
	}
	groups := config["proxy-groups"].([]any)
	if len(groups) != 3 {
		t.Fatalf("expected selector and two automatic groups: %#v", groups)
	}
	sg := groups[1].(map[string]any)
	if sg["name"] != "SG 优选 自动选择" || sg["type"] != "url-test" {
		t.Fatal(sg)
	}
	members := sg["proxies"].([]any)
	if len(members) != 2 || members[0] == members[1] {
		t.Fatalf("wrong or ambiguous members: %#v", members)
	}
	selector := groups[0].(map[string]any)["proxies"].([]any)
	found := false
	for _, item := range selector {
		if item == sg["name"] {
			found = true
		}
	}
	if !found {
		t.Fatal("auto group not selectable")
	}
	if binary := os.Getenv("TML_TEST_MIHOMO"); binary != "" {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, r.Content, 0600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(binary, "-t", "-f", path, "-d", dir).CombinedOutput(); err != nil {
			t.Fatalf("Mihomo rejected configuration: %s: %v", output, err)
		}
	}
}
