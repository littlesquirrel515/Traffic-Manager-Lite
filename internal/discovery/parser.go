package discovery

import "traffic-manager-lite/internal/inboundconfig"

type Discovered = inboundconfig.Discovered

var Parse = inboundconfig.Parse
var ID = inboundconfig.ID

func first(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
