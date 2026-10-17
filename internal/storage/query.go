package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"traffic-manager-lite/internal/core"
)

func mkdir(p string) error { return os.MkdirAll(p, 0700) }
func backupPath(p string, t time.Time) string {
	return filepath.Join(p, "traffic-"+t.UTC().Format("20060102-150405.000000000")+".db")
}
func (s *Store) Instances(ctx context.Context) ([]core.Instance, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id,server_id,name,core_type,api_endpoint,api_secret,config_path,version,enabled,last_collected_at,last_error,capabilities_json,control_endpoint,detected_version,clash_endpoint,clash_secret,(SELECT enabled FROM servers WHERE id=instances.server_id) FROM instances ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []core.Instance{}
	for rows.Next() {
		var i core.Instance
		var cap string
		e = rows.Scan(&i.ID, &i.ServerID, &i.Name, &i.CoreType, &i.APIEndpoint, &i.APISecret, &i.ConfigPath, &i.Version, &i.Enabled, &i.LastCollectedAt, &i.LastError, &cap, &i.ControlEndpoint, &i.DetectedVersion, &i.ClashEndpoint, &i.ClashSecret, &i.ServerEnabled)
		if e != nil {
			return nil, e
		}
		decode(cap, &i.Capabilities)
		out = append(out, i)
	}
	return out, rows.Err()
}

type Filter struct {
	Cycle                        bool
	From, To, Dimension, Scope   string
	UserID, InstanceID, ServerID int64
	Inbound, Protocol, NodeID    string
}

func (s *Store) Traffic(ctx context.Context, f Filter, history bool) ([]map[string]any, error) {
	matchedClients := ` FROM catalog_inbounds c JOIN inbound_client_keys k ON k.inbound_id=c.id JOIN credential_material a ON a.id=k.credential_id WHERE c.instance_id=i.instance_id AND a.core_client_ref=i.core_user_key AND (i.inbound_tag='' OR c.tag=i.inbound_tag) AND c.present=1`
	matchedNodes := ` FROM node_profiles n JOIN catalog_inbounds c ON c.id=n.inbound_id JOIN inbound_client_keys k ON k.inbound_id=c.id JOIN credential_material a ON a.id=k.credential_id WHERE c.instance_id=i.instance_id AND a.core_client_ref=i.core_user_key AND (i.inbound_tag='' OR c.tag=i.inbound_tag) AND c.present=1`
	dims := map[string]string{"user": "COALESCE(CAST(i.user_id AS TEXT),'未归属')", "instance": "CAST(i.instance_id AS TEXT)", "server": "CAST(x.server_id AS TEXT)", "inbound": "i.inbound_tag", "core": "x.core_type", "protocol": "(SELECT CASE WHEN count(DISTINCT c.protocol)=1 THEN min(c.protocol) ELSE '未归属' END" + matchedClients + ")", "node": "(SELECT CASE WHEN count(DISTINCT n.id)=1 THEN min(n.id) ELSE '未归属' END" + matchedNodes + ")"}
	dim := "'total'"
	if f.Dimension != "" {
		var ok bool
		dim, ok = dims[f.Dimension]
		if !ok {
			return nil, fmt.Errorf("invalid dimension")
		}
	}
	if f.Scope == "" {
		f.Scope = "user"
	}
	if f.Scope != "user" && f.Scope != "inbound" && f.Scope != "instance" {
		return nil, fmt.Errorf("invalid scope")
	}
	q := `SELECT ` + dim + ` AS dimension,COALESCE(SUM(t.upload_bytes),0) upload,COALESCE(SUM(t.download_bytes),0) download FROM traffic_daily t JOIN identities i ON i.id=t.identity_id JOIN instances x ON x.id=i.instance_id WHERE i.scope=?`
	if f.Cycle {
		q = strings.Replace(q, "FROM traffic_daily t", `FROM (SELECT s.identity_id,s.instance_id,s.date,s.upload_delta upload_bytes,s.download_delta download_bytes FROM traffic_samples s JOIN collection_batches b ON b.instance_id=s.instance_id AND b.batch_id=s.batch_id) t`, 1)
	}
	args := []any{f.Scope}
	for _, v := range []struct {
		cond    string
		val     any
		enabled bool
	}{{"t.date>=?", f.From, f.From != ""}, {"t.date<=?", f.To, f.To != ""}, {"i.user_id=?", f.UserID, f.UserID > 0}, {"i.instance_id=?", f.InstanceID, f.InstanceID > 0}, {"x.server_id=?", f.ServerID, f.ServerID > 0}, {"i.inbound_tag=?", f.Inbound, f.Inbound != ""}, {dims["node"] + "=?", f.NodeID, f.NodeID != ""}, {dims["protocol"] + "=?", f.Protocol, f.Protocol != ""}} {
		if v.enabled {
			q += " AND " + v.cond
			args = append(args, v.val)
		}
	}
	if history {
		q = strings.Replace(q, " AS dimension,", " AS dimension,t.date,", 1)
		q += " GROUP BY dimension,t.date ORDER BY t.date,dimension"
	} else {
		q += " GROUP BY dimension ORDER BY upload+download DESC"
	}
	return s.Rows(ctx, q, args...)
}
