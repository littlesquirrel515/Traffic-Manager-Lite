package httpapi

import (
	"net/http"
	"traffic-manager-lite/internal/coremonitor"
)

// Quick checks use actual read-only APIs and never advance traffic cursors.
func (a *API) testInstance(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	service := coremonitor.Service{Store: a.Store, Config: a.Config}
	instance, e := service.Instance(r.Context(), id)
	if e != nil {
		result(w, nil, e)
		return
	}
	report, e := service.Adapter(instance).CheckAPI(r.Context())
	if e != nil {
		result(w, nil, e)
		return
	}
	capabilities := []map[string]string{}
	count := 0
	for _, check := range report.Checks {
		capabilities = append(capabilities, map[string]string{"metric": check.API, "status": check.Status, "reason": check.Reason})
		if check.Group == "stats" {
			count += check.Count
		}
	}
	result(w, map[string]any{"connected": report.Connected == "Available", "records": count, "capabilities": capabilities, "report": report}, nil)
}
