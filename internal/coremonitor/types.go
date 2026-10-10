package coremonitor

import (
	"context"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/xraymonitor"
)

// Reports share the established Xray wire format; each core uses its own protocols.
type Report = xraymonitor.Report
type Check = xraymonitor.Check
type Health = xraymonitor.Health
type Client = xraymonitor.Client
type VersionInfo struct{ Version, Build, Source, Reason string }
type CoreDiagnostics interface {
	DetectVersion(context.Context) (VersionInfo, error)
	CheckAPI(context.Context) (Report, error)
	Diagnose(context.Context) (Report, error)
}
type Service struct {
	Store  *storage.Store
	Config config.Config
}
type observation struct {
	report                  Report
	clients                 []Client
	clientsOK               bool
	configRevision          string
	providerSnapshots       []core.ProviderSnapshot
	records                 []core.TrafficRecord
	online                  []core.OnlineRecord
	statsOK, onlineOK       bool
	userStatsOK             bool
	onlineKind, onlineBasis string
	states                  map[string]*xraymonitor.State
}
type Hysteria2Diagnostics struct {
	Service
	Instance core.Instance
}
type SingBoxDiagnostics struct {
	Service
	Instance core.Instance
}
type V2FlyDiagnostics struct {
	Service
	Instance core.Instance
}
type XrayDiagnostics struct {
	Service
	Instance core.Instance
}

func (s Service) Adapter(i core.Instance) CoreDiagnostics {
	switch i.CoreType {
	case "xray":
		return XrayDiagnostics{s, i}
	case "hysteria2":
		return Hysteria2Diagnostics{s, i}
	case "singbox":
		return SingBoxDiagnostics{s, i}
	default:
		return V2FlyDiagnostics{s, i}
	}
}
func (d Hysteria2Diagnostics) DetectVersion(ctx context.Context) (VersionInfo, error) {
	return d.Service.detectVersion(ctx, d.Instance)
}
func (d SingBoxDiagnostics) DetectVersion(ctx context.Context) (VersionInfo, error) {
	return d.Service.detectVersion(ctx, d.Instance)
}
func (d V2FlyDiagnostics) DetectVersion(ctx context.Context) (VersionInfo, error) {
	return d.Service.detectVersion(ctx, d.Instance)
}
func (d XrayDiagnostics) DetectVersion(ctx context.Context) (VersionInfo, error) {
	r := (xraymonitor.Service{Store: d.Store, Config: d.Config}).VersionEvidence(d.Instance)
	return VersionInfo{r.Version, r.Build, r.VersionSource, r.VersionReason}, nil
}
func (d Hysteria2Diagnostics) Diagnose(ctx context.Context) (Report, error) {
	return d.Observe(ctx, d.Instance, true, "manual")
}
func (d SingBoxDiagnostics) Diagnose(ctx context.Context) (Report, error) {
	return d.Observe(ctx, d.Instance, true, "manual")
}
func (d V2FlyDiagnostics) Diagnose(ctx context.Context) (Report, error) {
	return d.Observe(ctx, d.Instance, true, "manual")
}
func (d XrayDiagnostics) Diagnose(ctx context.Context) (Report, error) {
	return d.Observe(ctx, d.Instance, true, "manual")
}
func (d Hysteria2Diagnostics) CheckAPI(ctx context.Context) (Report, error) {
	return d.quick(ctx, d.Instance)
}
func (d SingBoxDiagnostics) CheckAPI(ctx context.Context) (Report, error) {
	return d.quick(ctx, d.Instance)
}
func (d V2FlyDiagnostics) CheckAPI(ctx context.Context) (Report, error) {
	return d.quick(ctx, d.Instance)
}
func (d XrayDiagnostics) CheckAPI(ctx context.Context) (Report, error) {
	return d.quick(ctx, d.Instance)
}
