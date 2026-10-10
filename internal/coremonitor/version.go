package coremonitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"google.golang.org/protobuf/types/known/emptypb"
	"os"
	"regexp"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
	native "traffic-manager-lite/internal/proto/singboxnative"
)

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.]+)?$`)

func validVersion(v string) bool { return len(v) < 64 && versionPattern.MatchString(v) }
func (s Service) detectVersion(ctx context.Context, i core.Instance) (VersionInfo, error) {
	v := s.evidence(i)
	if i.CoreType == "singbox" {
		s.bounded(ctx, func(c context.Context) {
			conn, e := s.dial(c, i, nativeEndpoint(i))
			if e != nil {
				return
			}
			defer conn.Close()
			r, e := native.NewStartedServiceClient(conn).GetVersion(auth(c, i), &emptypb.Empty{})
			if e == nil && validVersion(r.Version) {
				v = VersionInfo{Version: r.Version, Build: "Unknown", Source: "daemon.StartedService/GetVersion"}
			}
		})
		if i.ClashEndpoint != "" {
			s.bounded(ctx, func(c context.Context) {
				secret := i.ClashSecret
				if secret == "" {
					secret = i.APISecret
				}
				header := ""
				if secret != "" {
					header = "Bearer " + secret
				}
				var response struct {
					Version string `json:"version"`
				}
				if e := s.get(c, i, i.ClashEndpoint, "/version", header, &response); e != nil {
					return
				}
				version := strings.TrimPrefix(response.Version, "sing-box ")
				if !validVersion(version) {
					return
				}
				if v.Version != "Unknown" && v.Version != version {
					v = VersionInfo{Version: "Unknown", Build: "Unknown", Source: "API mismatch", Reason: "原生/宿主证据与 Clash 版本不一致"}
					return
				}
				v = VersionInfo{Version: version, Build: "Unknown", Source: "sing-box Clash GET /version"}
			})
		}
	}
	return v, nil
}
func (s Service) evidence(i core.Instance) VersionInfo {
	v := VersionInfo{Version: "Unknown", Build: "Unknown", Source: "unavailable", Reason: "API 没有版本接口或未取得可信证据；人工声明与 latest 标签不作为运行版本"}
	if i.ConfigPath == "" {
		return v
	}
	path, e := discovery.SafePath(s.Config.ConfigRoot, i.ConfigPath+".version.json")
	if e != nil {
		return v
	}
	info, e := os.Stat(path)
	if e != nil || info.Size() > 8192 {
		return v
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return v
	}
	var proof struct {
		Core     string `json:"core_type"`
		API      string `json:"api_endpoint"`
		Output   string `json:"output"`
		Observed string `json:"observed_at"`
		SHA      string `json:"config_sha256"`
		Method   string `json:"method"`
	}
	if json.Unmarshal(b, &proof) != nil || proof.Core != i.CoreType || proof.API != i.APIEndpoint || (proof.Method != "docker_exec_core_version" && proof.Method != "controlled_core_version") {
		return v
	}
	at, e := time.Parse(time.RFC3339, proof.Observed)
	if e != nil || time.Since(at) > 24*time.Hour || at.After(time.Now().Add(time.Minute)) {
		v.Reason = "版本证据过期或时间不合法（有效期 24h）"
		return v
	}
	cp, e := discovery.SafePath(s.Config.ConfigRoot, i.ConfigPath)
	if e != nil {
		return v
	}
	info, e = os.Stat(cp)
	if e != nil || info.Size() > 4<<20 {
		return v
	}
	cb, e := os.ReadFile(cp)
	if e != nil {
		return v
	}
	sum := sha256.Sum256(cb)
	if hex.EncodeToString(sum[:]) != proof.SHA {
		v.Reason = "版本证据与当前配置文件哈希不匹配"
		return v
	}
	pattern := ""
	switch i.CoreType {
	case "v2fly":
		pattern = `(?m)^V2Ray ([0-9]+\.[0-9]+\.[0-9]+)`
	case "hysteria2":
		pattern = `(?m)^Version:\s+v?([0-9]+\.[0-9]+\.[0-9]+)`
	case "singbox":
		pattern = `(?m)^sing-box version ([0-9]+\.[0-9]+\.[0-9]+)`
	}
	if pattern == "" {
		return v
	}
	match := regexp.MustCompile(pattern).FindStringSubmatch(proof.Output)
	if len(match) != 2 {
		return v
	}
	return VersionInfo{Version: match[1], Build: "受控官方 version 输出；仅保留版本，未保存原始输出", Source: proof.Method + " / endpoint + config SHA256（24h 有效；需信任宿主维护者）"}
}
