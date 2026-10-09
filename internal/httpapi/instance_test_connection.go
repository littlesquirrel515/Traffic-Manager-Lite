package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"traffic-manager-lite/internal/adapters"
	"traffic-manager-lite/internal/security"
)

// testInstance calls the actual read-only statistics API without updating cursors or totals.
func (a *API) testInstance(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		result(w, nil, e)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.Config.Timeout)
	defer cancel()
	instances, e := a.Store.Instances(ctx)
	if e != nil {
		result(w, nil, e)
		return
	}
	for _, i := range instances {
		if i.ID != id {
			continue
		}
		p := (security.Policy{Allowed: a.Config.AllowedTargets}).ForEndpoints(i.APIEndpoint, i.ControlEndpoint)
		for _, endpoint := range []string{i.APIEndpoint, i.ControlEndpoint} {
			if endpoint != "" {
				if e := p.Check(ctx, security.TargetAddress(endpoint)); e != nil {
					result(w, nil, e)
					return
				}
			}
		}
		adapter, e := adapters.New(i, p)
		if e != nil {
			result(w, nil, e)
			return
		}
		defer adapter.Close()
		records, e := adapter.CollectTraffic(ctx)
		if e != nil {
			result(w, nil, e)
			return
		}
		result(w, map[string]any{"connected": true, "records": len(records), "capabilities": adapter.Capabilities()}, nil)
		return
	}
	result(w, nil, sql.ErrNoRows)
}
