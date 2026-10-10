package xraymonitor

import "traffic-manager-lite/internal/core"

type Client struct {
	Inbound     string `json:"inbound"`
	Key         string `json:"asset_key"`
	Email       string `json:"email"`
	Protocol    string `json:"protocol"`
	Level       uint32 `json:"level"`
	Source      string `json:"source"`
	Fingerprint string `json:"-"`
}
type Check struct {
	Provider          string `json:"provider_type"`
	Scope             string `json:"metric_scope"`
	Direction         string `json:"traffic_direction"`
	Evidence          string `json:"evidence"`
	Response          string `json:"response_summary"`
	ApplicableVersion string `json:"applicable_version"`
	Group             string `json:"group"`
	API               string `json:"api"`
	Method            string `json:"method"`
	Params            string `json:"params"`
	Status            string `json:"status"`
	Count             int    `json:"count"`
	Requests          int    `json:"requests"`
	DurationMS        int64  `json:"duration_ms"`
	Code              string `json:"code"`
	Reason            string `json:"reason"`
	Required          string `json:"required"`
	Advice            string `json:"advice"`
	CheckedAt         string `json:"checked_at"`
}
type Health struct {
	Collector string `json:"collector"`
	Status    string `json:"status"`
	Count     int    `json:"count"`
	Code      string `json:"code"`
	Summary   string `json:"summary"`
}
type State struct {
	Email  string `json:"email"`
	Stats  string `json:"stats"`
	Online string `json:"online"`
	Count  *int64 `json:"count"`
	Basis  string `json:"basis"`
}
type Report struct {
	CoreType       string   `json:"core_type"`
	APITypes       []string `json:"api_types"`
	Authentication string   `json:"authentication"`
	VersionReason  string   `json:"version_reason"`
	InstanceID     int64    `json:"instance_id"`
	CheckedAt      string   `json:"checked_at"`
	Version        string   `json:"version"`
	Build          string   `json:"build_info"`
	VersionSource  string   `json:"version_source"`
	ConfigSource   string   `json:"config_source"`
	Connected      string   `json:"connected"`
	Checks         []Check  `json:"checks"`
	Health         []Health `json:"health"`
	// No credentials or IP lists enter diagnostic history.
}
type observation struct {
	fingerprintKey []byte
	report         Report
	clients        []Client
	configClients  []Client
	completeTags   map[string]bool
	listedTags     map[string]bool
	listOK         bool
	configOK       bool
	records        []core.TrafficRecord
	states         map[string]*State
	online         []core.OnlineRecord
}
