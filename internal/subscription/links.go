package subscription

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"traffic-manager-lite/internal/catalog"
	"traffic-manager-lite/internal/secretstore"
	"traffic-manager-lite/internal/storage"
)

type linkSecret struct {
	SubscriptionID string `json:"subscription_id"`
	Token          string `json:"token"`
}
type AdminLink struct {
	SubscriptionID string `json:"subscription_id"`
	Token          string `json:"token"`
	Created        bool   `json:"created"`
}

// VisibleLink preserves the legacy hash-only token, and lazily adds one encrypted
// administrator-visible alias. The alias has exactly the same resource scope.
func VisibleLink(ctx context.Context, s *storage.Store, keyfile, id string) (AdminLink, error) {
	key, e := secretstore.Key(keyfile)
	if e != nil {
		return AdminLink{}, fmt.Errorf("订阅链接查看需要可用的 TML_MANAGED_KEY_FILE 加密密钥")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return AdminLink{}, e
	}
	defer tx.Rollback()
	r, e := visibleLink(ctx, tx, key, id)
	if e != nil {
		return r, e
	}
	return r, tx.Commit()
}
func visibleLink(ctx context.Context, tx *sql.Tx, key []byte, id string) (AdminLink, error) {
	r := AdminLink{SubscriptionID: id}
	var subscriber string
	if e := tx.QueryRowContext(ctx, "SELECT subscriber_id FROM catalog_subscriptions WHERE id=?", id).Scan(&subscriber); e != nil {
		return r, e
	}
	var sealed, hash string
	e := tx.QueryRowContext(ctx, "SELECT token_sealed,token_hash FROM subscription_link_secrets WHERE subscription_id=?", id).Scan(&sealed, &hash)
	if e == nil {
		raw, err := secretstore.Open(key, sealed)
		if err != nil {
			return r, fmt.Errorf("订阅链接解密失败，请恢复原加密密钥；不会自动重置链接")
		}
		var secret linkSecret
		if json.Unmarshal(raw, &secret) != nil || secret.SubscriptionID != id || len(secret.Token) != 43 || Hash(secret.Token) != hash {
			return r, fmt.Errorf("订阅链接凭据校验失败")
		}
		r.Token = secret.Token
	} else if e == sql.ErrNoRows {
		token, h, err := Token()
		if err != nil {
			return r, err
		}
		if err = saveLinkSecret(ctx, tx, key, id, token, h); err != nil {
			return r, err
		}
		r.Token = token
		r.Created = true
	} else {
		return r, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO subscription_admin_audit(subscription_id,subscriber_id,action,created_at) VALUES(?,?,'view_link',?)", id, subscriber, storage.Stamp(time.Now()))
	return r, e
}
func saveLinkSecret(ctx context.Context, tx *sql.Tx, key []byte, id, token, hash string) error {
	raw, _ := json.Marshal(linkSecret{SubscriptionID: id, Token: token})
	sealed, e := secretstore.Seal(key, raw)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO subscription_link_secrets VALUES(?,?,?,?) ON CONFLICT(subscription_id) DO UPDATE SET token_hash=excluded.token_hash,token_sealed=excluded.token_sealed,created_at=excluded.created_at", id, hash, sealed, storage.Stamp(time.Now()))
	return e
}

// UnifiedLink owns one stable, auto-format delivery per Subscriber. Its node list
// is resolved from current permissions on every request; old deliveries survive.
func UnifiedLink(ctx context.Context, s *storage.Store, keyfile, subscriber string) (AdminLink, error) {
	key, e := secretstore.Key(keyfile)
	if e != nil {
		return AdminLink{}, fmt.Errorf("请配置独立加密密钥后查看统一订阅链接")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return AdminLink{}, e
	}
	defer tx.Rollback()
	var name string
	if e = tx.QueryRowContext(ctx, "SELECT name FROM subscribers WHERE id=?", subscriber).Scan(&name); e != nil {
		return AdminLink{}, e
	}
	var id string
	e = tx.QueryRowContext(ctx, "SELECT subscription_id FROM subscriber_unified_links WHERE subscriber_id=?", subscriber).Scan(&id)
	if e == sql.ErrNoRows {
		id = catalog.ID()
		token, hash, err := Token()
		if err != nil {
			return AdminLink{}, err
		}
		now := storage.Stamp(time.Now())
		if _, e = tx.ExecContext(ctx, `INSERT INTO catalog_subscriptions(id,subscriber_id,name,token_hash,enabled,format,options_json,created_at,updated_at) VALUES(?,?,?,?,1,'auto',?,?,?)`, id, subscriber, name+" · 统一订阅", hash, jsonText(RenderOptions{Template: "full", GroupName: "节点选择", DefaultRoute: "proxy"}), now, now); e != nil {
			return AdminLink{}, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO subscriber_unified_links VALUES(?,?)", subscriber, id); e != nil {
			return AdminLink{}, e
		}
		if e = saveLinkSecret(ctx, tx, key, id, token, hash); e != nil {
			return AdminLink{}, e
		}
	} else if e != nil {
		return AdminLink{}, e
	}
	r, e := visibleLink(ctx, tx, key, id)
	if e != nil {
		return r, e
	}
	return r, tx.Commit()
}

// PersistVisibleToken retains the exact address initially returned for a newly
// created advanced delivery, so later administrator views return that address.
func PersistVisibleToken(ctx context.Context, s *storage.Store, keyfile, id, token string) error {
	key, e := secretstore.Key(keyfile)
	if e != nil {
		return fmt.Errorf("订阅链接加密密钥不可用")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var hash string
	if e = tx.QueryRowContext(ctx, "SELECT token_hash FROM catalog_subscriptions WHERE id=?", id).Scan(&hash); e != nil {
		return e
	}
	if len(token) != 43 || Hash(token) != hash {
		return fmt.Errorf("订阅链接凭据不匹配")
	}
	if e = saveLinkSecret(ctx, tx, key, id, token, hash); e != nil {
		return e
	}
	return tx.Commit()
}

func RotateVisibleLink(ctx context.Context, s *storage.Store, keyfile, id string) (AdminLink, error) {
	key, e := secretstore.Key(keyfile)
	if e != nil {
		return AdminLink{}, fmt.Errorf("订阅链接密钥不可用，未重置链接")
	}
	token, hash, e := Token()
	if e != nil {
		return AdminLink{}, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return AdminLink{}, e
	}
	defer tx.Rollback()
	var subscriber string
	if e = tx.QueryRowContext(ctx, "SELECT subscriber_id FROM catalog_subscriptions WHERE id=?", id).Scan(&subscriber); e != nil {
		return AdminLink{}, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE catalog_subscriptions SET token_hash=?,token_version=token_version+1,updated_at=? WHERE id=?", hash, storage.Stamp(time.Now()), id); e != nil {
		return AdminLink{}, e
	}
	if e = saveLinkSecret(ctx, tx, key, id, token, hash); e != nil {
		return AdminLink{}, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO subscription_admin_audit(subscription_id,subscriber_id,action,created_at) VALUES(?,?,'rotate_link',?)", id, subscriber, storage.Stamp(time.Now())); e != nil {
		return AdminLink{}, e
	}
	return AdminLink{SubscriptionID: id, Token: token}, tx.Commit()
}
