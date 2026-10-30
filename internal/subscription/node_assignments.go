package subscription

import (
	"context"
	"traffic-manager-lite/internal/catalog"
)

type NodeAssignment struct {
	SubscriberID   string `json:"subscriber_id"`
	SubscriberName string `json:"subscriber_name"`
	NodeID         string `json:"node_id"`
	NodeName       string `json:"node_name"`
	CreatedAt      string `json:"created_at"`
	Explicit       bool   `json:"explicit"`
}

// NodeAssignments reads saved business selections, including legacy subscription
// choices. It neither grants all nodes of an inbound nor changes legacy rights.
func NodeAssignments(ctx context.Context, q catalog.Query, subscriber string) ([]NodeAssignment, error) {
	rows, e := q.QueryContext(ctx, `WITH assigned(subscriber_id,node_id,created_at,explicit) AS (
 SELECT subscriber_id,node_id,created_at,1 FROM subscriber_node_grants
 UNION ALL
 SELECT s.subscriber_id,n.node_id,s.created_at,0 FROM catalog_subscriptions s JOIN subscribers u ON u.id=s.subscriber_id JOIN catalog_subscription_nodes n ON n.subscription_id=s.id WHERE u.node_authorization_required=0
 UNION ALL
 SELECT s.subscriber_id,m.node_id,s.created_at,0 FROM catalog_subscriptions s JOIN subscribers u ON u.id=s.subscriber_id JOIN catalog_subscription_groups g ON g.subscription_id=s.id JOIN node_group_members m ON m.group_id=g.group_id WHERE u.node_authorization_required=0
 ) SELECT a.subscriber_id,u.name,a.node_id,n.name,MIN(a.created_at),MAX(a.explicit) FROM assigned a JOIN subscribers u ON u.id=a.subscriber_id JOIN node_profiles n ON n.id=a.node_id WHERE (?='' OR a.subscriber_id=?) GROUP BY a.subscriber_id,a.node_id ORDER BY u.name,n.name,a.node_id`, subscriber, subscriber)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []NodeAssignment{}
	for rows.Next() {
		var n NodeAssignment
		if e = rows.Scan(&n.SubscriberID, &n.SubscriberName, &n.NodeID, &n.NodeName, &n.CreatedAt, &n.Explicit); e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
