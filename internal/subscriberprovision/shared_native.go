package subscriberprovision

import (
	"context"
	"fmt"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/subscription"
)

// A fallback must identify one real, saved native account, never an account
// generated for another subscriber. Ambiguity requires explicit administration.
const nativeCandidates = `SELECT c.id FROM inbound_client_keys k JOIN credential_material c ON c.id=k.credential_id
 WHERE k.inbound_id=? AND c.status='available' AND c.auth_json<>'{}'
 AND NOT EXISTS(SELECT 1 FROM subscriber_provisioning p WHERE p.credential_id=c.id AND p.origin='managed')
 AND NOT EXISTS(SELECT 1 FROM remote_runtime_users u WHERE u.inbound_id=k.inbound_id AND u.client_ref=k.client_ref)
 AND NOT EXISTS(SELECT 1 FROM hysteria_auth_accounts h WHERE h.credential_id=c.id AND h.origin='managed')
 ORDER BY c.id LIMIT 2`

// Repair a legacy grant with no credential binding, without reviving a binding
// deliberately disabled by an administrator or creating a new dynamic account.
func needsNativeBinding(ctx context.Context, q catalog.Query, subscriber, node string) (bool, error) {
	var inbound, kind string
	var bindings int
	err := q.QueryRowContext(ctx, `SELECT n.inbound_id,i.core_type,(SELECT count(*) FROM subscriber_credentials c WHERE c.subscriber_id=? AND c.inbound_id=n.inbound_id) FROM node_profiles n JOIN catalog_inbounds b ON b.id=n.inbound_id JOIN instances i ON i.id=b.instance_id WHERE n.id=?`, subscriber, node).Scan(&inbound, &kind, &bindings)
	if err != nil {
		return false, err
	}
	if bindings > 0 || kind == "xray" || kind == "hysteria2" && hysteriaReady(ctx, q, inbound) {
		return false, nil
	}
	id, err := nativeCandidate(ctx, q, inbound)
	return id != "", err
}

func nativeCandidate(ctx context.Context, q catalog.Query, inbound string) (string, error) {
	rows, err := q.QueryContext(ctx, nativeCandidates, inbound)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", nil
	}
	return ids[0], nil
}

func (s Service) bindSharedNative(ctx context.Context, subscriber string, t NodeTarget) error {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	credential, err := nativeCandidate(ctx, tx, t.InboundID)
	if err != nil {
		return err
	}
	if credential == "" || credential != t.CredentialID {
		return fmt.Errorf("原生账号已变化或存在多个候选，请重新预览")
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM subscriber_credentials WHERE subscriber_id=? AND inbound_id=?", subscriber, t.InboundID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM subscriber_credentials WHERE subscriber_id=? AND inbound_id=? AND credential_id=? AND enabled=1 AND usage_mode='shared_native' AND billable=0", subscriber, t.InboundID, credential).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("已有认证绑定变化，未覆盖管理员设置")
		}
		return tx.Commit()
	}
	now := storage.Stamp(time.Now())
	if _, err = tx.ExecContext(ctx, `INSERT INTO subscriber_credentials(id,subscriber_id,inbound_id,credential_id,enabled,created_at,billable,usage_mode) VALUES(?,?,?,?,1,?,0,'shared_native')`, catalog.ID(), subscriber, t.InboundID, credential, now); err != nil {
		return err
	}
	// A shared account is delivery-only: never transfer or duplicate its meter.
	if err = subscription.RememberUsageSources(ctx, tx, subscriber, false); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO subscriber_quota_audit(subscriber_id,action,before_json,after_json,created_at) VALUES(?,'shared_native_delivery','{}',?,?)`, subscriber, `{"usage_mode":"shared_native","individual_metering":false}`, now); err != nil {
		return err
	}
	return tx.Commit()
}
