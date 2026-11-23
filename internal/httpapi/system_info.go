package httpapi

import (
	"net/http"
	"net/netip"
	"path/filepath"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/pkg/ipgeo"
	"traffic-manager-lite/pkg/systeminfo"
)

// The application adapter supplies local paths and protects the endpoint. The
// reusable collector has no dependency on TML's database or business services.
func systemInfoCollector(c config.Config) *systeminfo.Collector {
	paths := []systeminfo.DiskPath{{Name: "root", Path: string(filepath.Separator)}}
	if c.DBPath != "" {
		paths = append(paths, systeminfo.DiskPath{Name: "data", Path: filepath.Dir(c.DBPath)})
	}
	if c.BackupDir != "" {
		paths = append(paths, systeminfo.DiskPath{Name: "backups", Path: c.BackupDir})
	}
	files := []systeminfo.DiskPath{}
	if c.DBPath != "" {
		files = append(files, systeminfo.DiskPath{Name: "database", Path: c.DBPath}, systeminfo.DiskPath{Name: "wal", Path: c.DBPath + "-wal"}, systeminfo.DiskPath{Name: "shm", Path: c.DBPath + "-shm"})
	}
	return systeminfo.New(systeminfo.Options{Disks: paths, Files: files, HostNetworkDir: c.SystemHostNetworkDir, HostOSFile: c.SystemHostOSFile, PublicIPs: c.SystemPublicIPs})
}

func (a *API) systemInfoRoutes(admin *http.ServeMux) {
	collector := systemInfoCollector(a.Config)
	options := ipgeo.Options{DisableOnline: a.Config.SystemDisableOnlineIP, DatabasePath: a.Config.SystemGeoIPDatabase}
	for _, raw := range a.Config.SystemPublicIPs {
		if ip, err := netip.ParseAddr(raw); err == nil {
			options.Declared = append(options.Declared, ip)
		}
	}
	resolver := ipgeo.New(options)
	admin.HandleFunc("GET /api/v1/system/info", func(w http.ResponseWriter, r *http.Request) {
		snapshot := collector.Snapshot(r.Context())
		var local []netip.Addr
		for _, address := range snapshot.HostIPs {
			if address.Scope == "global" {
				if ip, err := netip.ParseAddr(address.IP); err == nil {
					local = append(local, ip)
				}
			}
		}
		respond(w, http.StatusOK, struct {
			systeminfo.Snapshot
			PublicNetwork ipgeo.Snapshot `json:"public_network"`
		}{snapshot, resolver.Current(local)})
	})
}
