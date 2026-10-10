package coremonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"io"
	"net/http"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
)

type apiError struct{ State, Code string }

func (e apiError) Error() string { return e.State + ": " + e.Code }
func classify(err error, version string) (string, string, string) {
	if err == nil {
		return "Available", "OK", "请求成功；空结果也代表接口可用"
	}
	var a apiError
	if errors.As(err, &a) {
		reason := "接口调用异常；未记录响应体或凭据"
		switch a.State {
		case "AuthenticationFailed":
			reason = "API 认证/授权失败（HTTP " + a.Code + "），核对该接口使用的 Secret"
		case "Unreachable":
			reason = "无法连接或请求超时；核对容器网络、DNS 与端口监听"
		case "Unknown":
			reason = "未配置此 API 或没有足够证据，不能判定官方不支持"
		}
		if a.Code == "InvalidJSON" {
			reason = "API 返回内容无法解析为所需 JSON，核对是否连接到正确的管理接口"
		}
		if a.Code == "ResponseLimit" {
			reason = "API 响应超过 4 MiB 或读取失败"
		}
		if a.State == "Error" && len(a.Code) == 3 {
			reason = "HTTP API 返回状态 " + a.Code + "；未保存响应体"
		}
		return a.State, a.Code, reason
	}
	code := status.Code(err)
	switch code {
	case codes.Unauthenticated, codes.PermissionDenied:
		return "AuthenticationFailed", code.String(), "认证或授权失败"
	case codes.Unavailable, codes.DeadlineExceeded:
		return "Unreachable", code.String(), "无法连接或请求超时"
	case codes.Unimplemented:
		if strings.HasPrefix(status.Convert(err).Message(), "unknown service ") && (version == "1.14.3" || version == "1.14.0" || version == "5.53.0" || version == "2.13.0") {
			return "Disabled", code.String(), "已核实的 API 服务未在当前端点注册"
		}
		return "Unknown", code.String(), "端点未提供方法，无法仅凭 Unimplemented 区分未启用与不支持"
	default:
		return "Error", code.String(), "接口调用或数据校验失败；未记录服务端错误体"
	}
}
func (o *observation) check(group, api, method, params, required string, start time.Time, n int, e error) Check {
	state, code, reason := classify(e, o.report.Version)
	c := Check{Group: group, API: api, Method: method, Params: params, Status: state, Count: n, Requests: 1, DurationMS: time.Since(start).Milliseconds(), Code: code, Reason: reason, Required: required, CheckedAt: storage.Stamp(time.Now()), ApplicableVersion: o.report.Version, Evidence: "实际只读请求", Response: fmt.Sprintf("%d 项；%s", n, code)}
	if e != nil {
		c.Advice = "检查对应 API 的监听地址、Docker 网络、Secret 及实际加载配置"
	}
	o.report.Checks = append(o.report.Checks, c)
	return c
}
func (o *observation) note(group, api, state, reason, required string) {
	o.report.Checks = append(o.report.Checks, Check{Group: group, API: api, Status: state, Method: "未调用：官方 schema 或配置依据", Reason: reason, Required: required, CheckedAt: o.report.CheckedAt, ApplicableVersion: o.report.Version, Evidence: "已核对正式版官方 schema；未用统计用户替代资产", Response: "未请求"})
}
func (s Service) bounded(ctx context.Context, fn func(context.Context)) {
	limit := s.Config.Timeout
	if limit > 3*time.Second {
		limit = 3 * time.Second
	}
	c, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	fn(c)
}
func (s Service) policy(i core.Instance) security.Policy {
	return (security.Policy{Allowed: s.Config.AllowedTargets}).ForEndpoints(i.APIEndpoint, i.ControlEndpoint, i.ClashEndpoint)
}
func (s Service) dial(ctx context.Context, i core.Instance, endpoint string) (*grpc.ClientConn, error) {
	if endpoint == "" {
		return nil, apiError{"Unknown", "NotConfigured"}
	}
	if e := security.ValidateEndpoint(endpoint, false); e != nil {
		return nil, apiError{"Error", "InvalidTarget"}
	}
	p := s.policy(i)
	if e := p.Check(ctx, security.TargetAddress(endpoint)); e != nil {
		return nil, apiError{"Unreachable", "TargetPolicyOrDNS"}
	}
	return grpc.NewClient("passthrough:///"+endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(p.Dial), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20)))
}
func (s Service) get(ctx context.Context, i core.Instance, base, path, authorization string, out any) error {
	if base == "" {
		return apiError{"Unknown", "NotConfigured"}
	}
	if e := security.ValidateEndpoint(base, true); e != nil {
		return apiError{"Error", "InvalidTarget"}
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if e != nil {
		return apiError{"Error", "InvalidTarget"}
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	client := s.policy(i).HTTP()
	defer client.CloseIdleConnections()
	response, e := client.Do(request)
	if e != nil {
		return apiError{"Unreachable", "NetworkOrTimeout"}
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return apiError{"AuthenticationFailed", fmt.Sprint(response.StatusCode)}
	}
	if response.StatusCode != 200 {
		return apiError{"Error", fmt.Sprint(response.StatusCode)}
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if e != nil || len(b) > 4<<20 {
		return apiError{"Error", "ResponseLimit"}
	}
	if json.Unmarshal(b, out) != nil {
		return apiError{"Error", "InvalidJSON"}
	}
	return nil
}
