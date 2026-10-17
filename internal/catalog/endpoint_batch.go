package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"traffic-manager-lite/internal/storage"
)

type EndpointBatch struct {
	IDs           []string       `json:"ids"`
	Enabled       *bool          `json:"enabled,omitempty"`
	Profile       map[string]any `json:"profile,omitempty"`
	Sort          *int           `json:"sort_order,omitempty"`
	ConfirmShared bool           `json:"confirm_shared"`
}

func ReadEndpoint(ctx context.Context, q Query, id string) (Endpoint, error) {
	var n Endpoint
	var raw string
	e := q.QueryRowContext(ctx, "SELECT id,name,profile_json,enabled,sort_order,description,endpoint_type FROM node_endpoints WHERE id=?", id).Scan(&n.ID, &n.Name, &raw, &n.Enabled, &n.Sort, &n.Description, &n.Kind)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &n.Profile)
	}
	return n, e
}

// BatchEndpoints commits all changes together, including shared-impact checks.
func BatchEndpoints(ctx context.Context, s *storage.Store, d EndpointBatch) error {
	if len(d.IDs) == 0 || len(d.IDs) > 500 || !d.ConfirmShared {
		return errors.New("请选择至多500个入口并确认共享影响")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	for i, id := range d.IDs {
		if seen[id] {
			return errors.New("入口重复")
		}
		seen[id] = true
		n, err := ReadEndpoint(ctx, tx, id)
		if err != nil {
			return err
		}
		if d.Enabled != nil {
			n.Enabled = *d.Enabled
		}
		if d.Sort != nil {
			n.Sort = *d.Sort + i
		}
		for k, v := range d.Profile {
			n.Profile[k] = v
		}
		n.ConfirmShared = true
		if _, err = saveEndpoint(ctx, tx, n); err != nil {
			return err
		}
	}
	return tx.Commit()
}
