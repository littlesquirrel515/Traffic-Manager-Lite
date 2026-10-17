package remote

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"time"
	"traffic-manager-lite/internal/core"
	hp "traffic-manager-lite/internal/proto/xrayhandler"
	"traffic-manager-lite/internal/secretstore"
	"traffic-manager-lite/internal/storage"
)

// RecheckRuntime runs independently of Stats. Recovery is opt-in, Xray-only,
// read-before-add, and never removes/replaces a conflicting runtime user.
// The endpoint/tag/protocol evidence must still match the approved target.
func RecheckRuntime(ctx context.Context, s *storage.Store, i core.Instance, keyFile string, allowed []string) {
	if i.CoreType != "xray" || keyFile == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	rows, e := s.DB.QueryContext(ctx, `SELECT r.inbound_id,r.client_ref,r.desired_sealed,r.desired_enabled,r.restore_authorized,r.api_endpoint,r.core_tag,r.protocol,r.revision,c.tag,c.protocol,c.retired,r.status FROM remote_runtime_users r JOIN catalog_inbounds c ON c.id=r.inbound_id WHERE c.instance_id=? AND NOT EXISTS (SELECT 1 FROM subscriber_provisioning p JOIN subscribers owner ON owner.id=p.subscriber_id WHERE p.inbound_id=r.inbound_id AND p.client_ref=r.client_ref AND (owner.enabled=0 OR p.status!='ready')) AND (r.status NOT IN ('applying','restoring') OR r.checked_at<?) ORDER BY r.checked_at LIMIT 100`, i.ID, storage.Stamp(time.Now().Add(-30*time.Second)))
	if e != nil {
		return
	}
	type item struct {
		inbound, ref, sealed, endpoint, tag, protocol, currentTag, currentProtocol, originalStatus string
		enabled, restore, retired                                                                  bool
		revision                                                                                   int64
	}
	items := []item{}
	for rows.Next() {
		var v item
		if e = rows.Scan(&v.inbound, &v.ref, &v.sealed, &v.enabled, &v.restore, &v.endpoint, &v.tag, &v.protocol, &v.revision, &v.currentTag, &v.currentProtocol, &v.retired, &v.originalStatus); e != nil {
			break
		}
		items = append(items, v)
	}
	rows.Close()
	if e != nil || len(items) == 0 {
		return
	}
	key, e := secretstore.Key(keyFile)
	if e != nil {
		return
	}
	c, e := dialRuntime(ctx, i, allowed)
	if e != nil {
		return
	}
	defer c.Close()
	for _, v := range items {
		if ctx.Err() != nil {
			return
		}
		state := "unknown"
		if v.retired || v.endpoint != i.APIEndpoint || v.tag != v.currentTag || v.protocol != v.currentProtocol {
			state = "target_changed"
		} else {
			b, er := secretstore.Open(key, v.sealed)
			var desired Client
			if er == nil {
				er = json.Unmarshal(b, &desired)
			}
			if er == nil {
				actual, err := readUser(ctx, c, v.tag, v.ref)
				if err == nil {
					if !v.enabled {
						if actual == nil {
							state = "runtime_verified"
						} else {
							state = "undesired_present"
						}
					} else {
						account, er := runtimeAccount(i.CoreType, v.protocol, desired)
						if er == nil {
							if actual == nil && v.restore {
								// Claim the exact revision before writing. Concurrent explicit
								// operations cannot be overwritten by an older recovery plan.
								res, claim := s.DB.ExecContext(ctx, "UPDATE remote_runtime_users SET status='restoring' WHERE inbound_id=? AND client_ref=? AND revision=? AND status NOT IN ('applying','restoring')", v.inbound, v.ref, v.revision)
								n := int64(0)
								if claim == nil {
									n, _ = res.RowsAffected()
								}
								if n == 1 {
									v.originalStatus = "restoring"
									if er = alter(ctx, c, i.CoreType, v.tag, "add", &hp.User{Email: v.ref, Account: account, Level: desired.Level}); er == nil {
										actual, err = readUser(ctx, c, v.tag, v.ref)
									}
								}
							}
							if err == nil && actual != nil && proto.Equal(actual.Account, account) {
								state = "runtime_verified"
							} else if actual == nil {
								state = "runtime_missing"
							} else {
								state = "desired_mismatch"
							}
						}
					}
				}
			}
		}
		_, _ = s.DB.ExecContext(ctx, "UPDATE remote_runtime_users SET status=?,checked_at=? WHERE inbound_id=? AND client_ref=? AND revision=? AND status=?", state, storage.Stamp(time.Now()), v.inbound, v.ref, v.revision, v.originalStatus)
	}
}
