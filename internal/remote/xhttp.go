package remote

import (
	"fmt"
	"traffic-manager-lite/internal/core"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
)

// Decode the fixed official wire schema, not the presence of an `extra` JSON
// object (which the runtime API does not retain). Build() serializes defaults
// even when the operator has supplied only path/mode. Never export raw extras.
func decodeXHTTP(v *hp.XHTTPConfig, p *core.NodeProfile) {
	p.Host, p.Path, p.XHTTPMode = v.Host, v.Path, v.Mode
	limit := func(reason string) { p.Limitations = append(p.Limitations, "XHTTP "+reason) }
	if len(v.ProtoReflect().GetUnknown()) > 0 {
		limit("包含当前解析器未识别的字段，需核对客户端兼容性")
	}
	if len(v.Headers) > 0 {
		limit("自定义 headers 尚未安全映射，需核对客户端参数")
	}
	if v.NoGrpcHeader {
		value := true
		p.XHTTPNoGRPCHeader = &value
	}
	if r := v.XPaddingBytes; r != nil {
		if len(r.ProtoReflect().GetUnknown()) > 0 {
			limit("padding 包含未知字段")
		}
		if r.From != 0 || r.To != 0 {
			if r.From <= 0 || r.To < r.From || r.To > 1000000 {
				limit("padding 范围无效或超出客户端限制")
			} else {
				p.XHTTPPadding = fmt.Sprintf("%d-%d", r.From, r.To)
			}
		}
	}
	// Defaults from infra/conf/transport_internet.go v26.3.27. Customized
	// routing/obfuscation cannot be inferred from an inbound for every renderer.
	defaults := [][3]string{
		{"xPaddingKey", v.XPaddingKey, "x_padding"}, {"xPaddingHeader", v.XPaddingHeader, "X-Padding"},
		{"xPaddingPlacement", v.XPaddingPlacement, "queryInHeader"}, {"xPaddingMethod", v.XPaddingMethod, "repeat-x"},
		{"uplinkHTTPMethod", v.UplinkHttpMethod, "POST"}, {"sessionPlacement", v.SessionPlacement, "path"},
		{"seqPlacement", v.SeqPlacement, "path"}, {"sessionKey", v.SessionKey, ""}, {"seqKey", v.SeqKey, ""},
	}
	for _, d := range defaults {
		if d[1] != "" && d[1] != d[2] {
			limit(d[0] + " 使用尚未映射的自定义值")
		}
	}
	if v.UplinkDataPlacement != "" && v.UplinkDataPlacement != "auto" && v.UplinkDataPlacement != "body" {
		limit("uplinkDataPlacement 需要专用客户端参数")
	}
	if v.UplinkDataKey != "" && v.UplinkDataKey != "X-Data" {
		limit("uplinkDataKey 使用尚未映射的自定义值")
	}
	if v.XPaddingObfsMode {
		limit("自定义 padding 混淆尚未映射")
	}
	if v.DownloadSettings != nil {
		limit("独立 downloadSettings 尚未映射")
	}
	if v.SessionIdTable != "" || (v.SessionIdLength != nil && (v.SessionIdLength.From != 0 || v.SessionIdLength.To != 0 || len(v.SessionIdLength.ProtoReflect().GetUnknown()) > 0)) {
		limit("自定义 sessionID 生成参数尚未映射")
	}
	// Empty range submessages and the official default Xmux are normal builder
	// output. Retain conservative filtering for customized client tuning.
	for _, r := range []*hp.XHTTPRange{v.ScMaxEachPostBytes, v.ScMinPostsIntervalMs, v.ScStreamUpServerSecs, v.UplinkChunkSize} {
		if r != nil && (r.From != 0 || r.To != 0 || len(r.ProtoReflect().GetUnknown()) > 0) {
			limit("自定义上传/分块调优尚未映射")
			break
		}
	}
	if m := v.Xmux; m != nil {
		match := func(r *hp.XHTTPRange, lo, hi int32) bool {
			return r == nil || (len(r.ProtoReflect().GetUnknown()) == 0 && ((r.From == 0 && r.To == 0) || (r.From == lo && r.To == hi)))
		}
		// 26.3.27 defaults to concurrency=1; 26.9.9 to connections=3.
		if len(m.ProtoReflect().GetUnknown()) > 0 || !match(m.MaxConcurrency, 1, 1) || !match(m.MaxConnections, 3, 3) || !match(m.CMaxReuseTimes, 0, 0) || !match(m.HMaxRequestTimes, 600, 900) || !match(m.HMaxReusableSecs, 1800, 3000) || m.HKeepAlivePeriod != 0 {
			limit("自定义连接复用参数尚未映射")
		}
	}
	// noSSEHeader, scMaxBufferedPosts and serverMaxHeaderBytes are server-side
	// response/buffering limits, not mandatory client configuration.
}
