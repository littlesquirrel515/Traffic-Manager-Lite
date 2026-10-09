package core

import (
	"context"
	"errors"
	"time"
)

var ErrUnsupported = errors.New("不支持此指标")

type Capability struct {
	Metric string `json:"metric"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}
type TrafficRecord struct {
	ServerID, InstanceID                        int64
	InboundTag, UserKey, Scope, Source, EpochID string
	UploadBytes, DownloadBytes                  int64
	CounterMode                                 string
	CollectedAt                                 time.Time
	BootEstimate                                *time.Time
}
type OnlineRecord struct {
	UserKey     string    `json:"user_key"`
	Count       int64     `json:"count"`
	Kind        string    `json:"kind"`
	IPs         []string  `json:"ips,omitempty"`
	CollectedAt time.Time `json:"collected_at"`
	Source      string    `json:"source"`
}
type Collector interface {
	Name() string
	Capabilities() []Capability
	CollectTraffic(context.Context) ([]TrafficRecord, error)
	CollectOnline(context.Context) ([]OnlineRecord, error)
	Close() error
}
type Instance struct {
	ID              int64        `json:"id"`
	ServerID        int64        `json:"server_id"`
	Name            string       `json:"name"`
	CoreType        string       `json:"core_type"`
	APIEndpoint     string       `json:"api_endpoint"`
	ControlEndpoint string       `json:"control_endpoint"`
	DetectedVersion string       `json:"detected_version"`
	APISecret       string       `json:"-"`
	ConfigPath      string       `json:"config_path"`
	Version         string       `json:"version"`
	Enabled         bool         `json:"enabled"`
	LastCollectedAt *string      `json:"last_collected_at"`
	LastError       string       `json:"last_error"`
	Capabilities    []Capability `json:"capabilities"`
}
type NodeProfile struct {
	Name        string   `json:"name"`
	Protocol    string   `json:"protocol"`
	Address     string   `json:"address"`
	Port        int      `json:"port"`
	ListenPort  int      `json:"listen_port,omitempty"`
	UserKey     string   `json:"user_key"`
	UUID        string   `json:"uuid,omitempty"`
	Password    string   `json:"password,omitempty"`
	Method      string   `json:"method,omitempty"`
	Transport   string   `json:"transport,omitempty"`
	TLS         bool     `json:"tls"`
	Reality     bool     `json:"reality,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
	SNI         string   `json:"sni,omitempty"`
	Host        string   `json:"host,omitempty"`
	Path        string   `json:"path,omitempty"`
	ServiceName string   `json:"service_name,omitempty"`
	PublicKey   string   `json:"public_key,omitempty"`
	ShortID     string   `json:"short_id,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Flow        string   `json:"flow,omitempty"`
	Sort        int      `json:"sort"`
	Enabled     bool     `json:"enabled"`
}
type Node struct {
	ID         string         `json:"id"`
	InstanceID int64          `json:"instance_id"`
	InboundTag string         `json:"inbound_tag"`
	UserID     int64          `json:"user_id"`
	Profile    NodeProfile    `json:"profile"`
	Complete   bool           `json:"complete"`
	Issues     []string       `json:"issues"`
	Overrides  map[string]any `json:"overrides"`
	SourceHash string         `json:"source_hash"`
	Present    bool           `json:"present"`
}
