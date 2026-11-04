package subscription

import "strings"

func ValidDeliveryFormat(f string) bool { return f == "auto" || ValidFormat(f) }
func RenderFormat(f string) string {
	if f == "auto" {
		return "base64"
	}
	return f
}

// RequestFormat is content negotiation, never authentication. Explicit query
// options win because all clients allow overriding User-Agent. Unknown agents
// use the delivery fallback, which is Base64 for unified subscriptions.
func RequestFormat(explicit, agent, accept string) string {
	if explicit != "" && explicit != "auto" {
		return explicit
	}
	ua := strings.ToLower(strings.TrimSpace(agent))
	for _, prefix := range []string{"clash-verge", "clashverge", "clash.meta", "clash-meta", "mihomo", "clash/", "clash "} {
		if strings.HasPrefix(ua, prefix) {
			return "mihomo"
		}
	}
	for _, prefix := range []string{"v2rayn", "v2rayng"} {
		if strings.HasPrefix(ua, prefix) {
			return "base64"
		}
	}
	if strings.HasPrefix(ua, "sing-box/") {
		return "singbox"
	}
	if strings.Contains(strings.ToLower(accept), "application/yaml") || strings.Contains(strings.ToLower(accept), "text/yaml") {
		return "mihomo"
	}
	return ""
}
