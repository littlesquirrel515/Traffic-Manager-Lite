package subscription

import (
	"context"
	"database/sql"
	"traffic-manager-lite/internal/storage"
)

// SubscriberPreview resolves current permissions without creating a delivery,
// exposing a token, decrypting a link secret, or calling a remote core.
func SubscriberPreview(ctx context.Context, s *storage.Store, subscriber string) (CatalogPreview, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return CatalogPreview{}, e
	}
	defer tx.Rollback()
	var name string
	if e = tx.QueryRowContext(ctx, "SELECT name FROM subscribers WHERE id=?", subscriber).Scan(&name); e != nil {
		return CatalogPreview{}, e
	}
	d := CatalogDefinition{SubscriberID: subscriber, Name: name, Unified: true, Format: "auto", Options: RenderOptions{Template: "full", GroupName: "节点选择", DefaultRoute: "proxy"}}
	var id string
	e = tx.QueryRowContext(ctx, "SELECT subscription_id FROM subscriber_unified_links WHERE subscriber_id=?", subscriber).Scan(&id)
	if e == nil {
		d, e = CatalogSubscription(ctx, tx, id)
		if e == sql.ErrNoRows {
			return CatalogPreview{}, ErrUnifiedLinkIncomplete
		}
		if e != nil {
			return CatalogPreview{}, e
		}
		if d.SubscriberID != subscriber || !d.Unified {
			return CatalogPreview{}, ErrUnifiedLinkIncomplete
		}
	} else if e == sql.ErrNoRows {
		nodes, err := NodeAssignments(ctx, tx, subscriber)
		if err != nil {
			return CatalogPreview{}, err
		}
		for _, node := range nodes {
			d.NodeIDs = append(d.NodeIDs, node.NodeID)
		}
	} else {
		return CatalogPreview{}, e
	}
	return CatalogPreviewFor(ctx, tx, d)
}
