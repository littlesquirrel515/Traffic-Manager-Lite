package httpapi

import (
	"net/http"
	"path/filepath"
	"traffic-manager-lite/internal/config"
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
	admin.HandleFunc("GET /api/v1/system/info", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, collector.Snapshot(r.Context()))
	})
}
