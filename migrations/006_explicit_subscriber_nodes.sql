-- Freeze legacy authority at saved assignment evidence, never at the current
-- membership of a group that may have acquired new nodes in the meantime.
-- A unified link's creation is the latest reliable legacy snapshot boundary;
-- without one, use each explicitly attached legacy subscription's creation.
INSERT OR IGNORE INTO subscriber_node_grants(subscriber_id,node_id,inbound_id,created_at)
SELECT c.subscriber_id,n.id,n.inbound_id,strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM catalog_subscriptions c
JOIN subscribers u ON u.id=c.subscriber_id
JOIN catalog_subscription_nodes sn ON sn.subscription_id=c.id
JOIN node_profiles n ON n.id=sn.node_id
WHERE u.node_authorization_required=0
AND NOT EXISTS(SELECT 1 FROM subscriber_node_revocations r WHERE r.subscriber_id=u.id AND r.node_id=n.id);

INSERT OR IGNORE INTO subscriber_node_grants(subscriber_id,node_id,inbound_id,created_at)
SELECT c.subscriber_id,n.id,n.inbound_id,strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM catalog_subscriptions c
JOIN subscribers u ON u.id=c.subscriber_id
JOIN catalog_subscription_groups sg ON sg.subscription_id=c.id
JOIN node_group_members gm ON gm.group_id=sg.group_id
JOIN node_profiles n ON n.id=gm.node_id
WHERE u.node_authorization_required=0
AND n.created_at<=COALESCE((SELECT MIN(s.created_at) FROM subscriber_unified_links l JOIN catalog_subscriptions s ON s.id=l.subscription_id WHERE l.subscriber_id=u.id),c.created_at)
AND NOT EXISTS(SELECT 1 FROM subscriber_node_revocations r WHERE r.subscriber_id=u.id AND r.node_id=n.id);

INSERT INTO subscriber_quota_audit(subscriber_id,action,before_json,after_json,created_at)
SELECT u.id,'explicit_node_authorization','{"legacy_dynamic_groups":true}',
json_object('explicit_nodes',(SELECT count(*) FROM subscriber_node_grants g WHERE g.subscriber_id=u.id),'group_expansion',false),strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM subscribers u WHERE u.node_authorization_required=0;
UPDATE subscribers SET node_authorization_required=1 WHERE node_authorization_required=0;
