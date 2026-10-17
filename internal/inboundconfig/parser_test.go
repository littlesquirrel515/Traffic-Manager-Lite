package inboundconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestXHTTPExtraCannotLeakSensitiveHeadersOrSilentlyOverride(t *testing.T) {
	for _, options := range []string{
		`{"path":"/x","headers":{"Authorization":"private-fixture-secret","X-Test":"ok"}}`,
		`{"path":"/x","extra":{"path":"/different"}}`,
		`{"path":"/x","extra":"invalid"}`,
		`{"path":"/x","headers":"invalid"}`,
	} {
		b := []byte(`{"inbounds":[{"tag":"xh","protocol":"vless","port":443,"settings":{"clients":[{"email":"alice","id":"fixture-id"}]},"streamSettings":{"network":"xhttp","security":"tls","xhttpSettings":` + options + `}}]}`)
		rows, e := Parse("xray", b)
		if e != nil || len(rows) != 1 {
			t.Fatal(e, len(rows))
		}
		p := rows[0].Profile
		serialized, _ := json.Marshal(p)
		if strings.Contains(string(serialized), "private-fixture-secret") {
			t.Fatal("sensitive header entered catalog profile")
		}
		if len(p.XHTTPLimitations) == 0 || p.Path != "/x" {
			t.Fatal("unsafe extension was accepted", p)
		}
		if len(p.Limitations) != 0 {
			t.Fatal("renderer limitation incorrectly made global node unavailable")
		}
	}
}
