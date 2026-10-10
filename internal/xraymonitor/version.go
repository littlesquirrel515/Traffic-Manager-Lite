package xraymonitor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/discovery"
)

// VersionEvidence reads a bounded, trusted host proof without calling the core.
func (s Service) VersionEvidence(inst core.Instance) Report {
	r := Report{CoreType: "xray", Version: "Unknown", Build: "Unknown", VersionSource: "unavailable", VersionReason: "API 无版本方法；未取得可信宿主证据"}
	s.versionEvidence(inst, &r)
	if r.Version != "Unknown" {
		r.VersionReason = ""
	}
	return r
}

// Version evidence is generated on the host by a fixed read-only docker exec script.
// The manager never mounts Docker socket or executes an administrator-supplied shell command.
func (s Service) versionEvidence(inst core.Instance, r *Report) {
	if inst.ConfigPath == "" {
		return
	}
	path, e := discovery.SafePath(s.Config.ConfigRoot, inst.ConfigPath+".version.json")
	if e != nil {
		return
	}
	info, e := os.Stat(path)
	if e != nil || info.Size() > 8192 {
		return
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return
	}
	var v struct {
		API       string `json:"api_endpoint"`
		Output    string `json:"output"`
		Observed  string `json:"observed_at"`
		ConfigSHA string `json:"config_sha256"`
		Method    string `json:"method"`
	}
	if json.Unmarshal(b, &v) != nil || v.API != inst.APIEndpoint || (v.Method != "docker_exec_xray_version" && v.Method != "controlled_xray_version") {
		return
	}
	at, e := time.Parse(time.RFC3339, v.Observed)
	if e != nil || time.Since(at) > 24*time.Hour || at.After(time.Now().Add(time.Minute)) {
		return
	}
	configPath, e := discovery.SafePath(s.Config.ConfigRoot, inst.ConfigPath)
	if e != nil {
		return
	}
	ci, e := os.Stat(configPath)
	if e != nil || ci.Size() > 4<<20 {
		return
	}
	cb, e := os.ReadFile(configPath)
	if e != nil {
		return
	}
	sum := sha256.Sum256(cb)
	if hex.EncodeToString(sum[:]) != v.ConfigSHA {
		return
	}
	match := regexp.MustCompile(`^Xray ([0-9]+\.[0-9]+\.[0-9]+) \(Xray, Penetrates Everything\.\) ([a-zA-Z0-9]+) \((go[0-9.]+) ([a-zA-Z0-9_/]+)\)`).FindStringSubmatch(v.Output)
	if len(match) != 5 {
		return
	}
	r.Version = match[1]
	r.Build = match[2] + " " + match[3] + " " + match[4]
	r.VersionSource = v.Method + " / host evidence（24h 有效；需信任宿主维护者）"
}
