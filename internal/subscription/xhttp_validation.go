package subscription

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"traffic-manager-lite/internal/core"
)

var headerName = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]+$`)
var paddingRange = regexp.MustCompile(`^[0-9]{1,7}(-[0-9]{1,7})?$`)

func ValidateXHTTP(p core.NodeProfile) string {
	if len(p.XHTTPHeaders) > 32 {
		return "XHTTP headers 超出限制"
	}
	for name, value := range p.XHTTPHeaders {
		key := http.CanonicalHeaderKey(name)
		if !headerName.MatchString(name) || len(name) > 100 || len(value) > 2048 || strings.ContainsAny(value, "\r\n\x00") || key == "Authorization" || key == "Proxy-Authorization" || key == "Cookie" || key == "Set-Cookie" || key == "X-Api-Key" || key == "X-Auth-Token" {
			return "XHTTP headers 含敏感或无效字段"
		}
	}
	if p.XHTTPPadding != "" {
		if !paddingRange.MatchString(p.XHTTPPadding) {
			return "XHTTP padding 范围无效"
		}
		v := strings.Split(p.XHTTPPadding, "-")
		lo, _ := strconv.Atoi(v[0])
		hi := lo
		if len(v) == 2 {
			hi, _ = strconv.Atoi(v[1])
		}
		if hi < lo || hi > 1000000 {
			return "XHTTP padding 范围无效"
		}
	}
	if p.Reality && p.PublicKey == "" {
		return "REALITY 缺少公钥"
	}
	if p.Reality {
		for _, alpn := range p.ALPN {
			if alpn == "h3" {
				return "Mihomo XHTTP HTTP/3 不支持 REALITY"
			}
		}
	}
	return ""
}
