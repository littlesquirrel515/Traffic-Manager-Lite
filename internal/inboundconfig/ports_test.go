package inboundconfig

import (
	"strings"
	"testing"
)

func TestOfficialXrayPortSpansAndCoreEnvironmentRemainUnexpanded(t *testing.T) {
	for _, ports := range []string{`"10000,10002-10004"`, `"env:XRAY_PORT"`} {
		rows, e := Sources("xray", []byte(`{"inbounds":[{"tag":"range","port":`+ports+`,"protocol":"vless","settings":{"clients":[]}}]}`))
		if e != nil || len(rows) != 1 || len(rows[0].Profile.ListenPorts) == 0 {
			t.Fatal(rows, e)
		}
		if strings.Contains(ports, "env:") && rows[0].Profile.ListenPorts[0] != "env:XRAY_PORT" {
			t.Fatal("core env evaluated by TML")
		}
	}
	for _, bad := range []string{`"100-50"`, `"0-200"`, `"200-65536"`, `"200,,300"`, `10443.1`} {
		if _, e := Sources("xray", []byte(`{"inbounds":[{"tag":"bad","port":`+bad+`,"protocol":"vless","settings":{"clients":[]}}]}`)); e == nil {
			t.Fatal("invalid range accepted", bad)
		}
	}
}
