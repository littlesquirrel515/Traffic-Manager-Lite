CREATE TABLE subscriber_group_grants (
 subscriber_id TEXT NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
 group_id TEXT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
 created_at TEXT NOT NULL,
 PRIMARY KEY(subscriber_id,group_id)
);
CREATE INDEX subscriber_group_grants_group ON subscriber_group_grants(group_id,subscriber_id);
UPDATE preferred_sync_jobs SET config_json=json_set(config_json,'$.follow_all',json('true'));
-- Legacy releases stored only per-node snapshots. A completely granted enabled
-- group corresponds to the whole-group selection users explicitly requested.
-- Partial or revoked groups are never promoted.
INSERT INTO subscriber_group_grants(subscriber_id,group_id,created_at)
SELECT u.id,g.id,strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM subscribers u CROSS JOIN node_groups g
WHERE u.node_authorization_required=1 AND g.enabled=1
 AND EXISTS (SELECT 1 FROM node_group_members m WHERE m.group_id=g.id)
 AND NOT EXISTS (SELECT 1 FROM node_group_members m WHERE m.group_id=g.id AND (
    NOT EXISTS (SELECT 1 FROM subscriber_node_grants a JOIN node_profiles n ON n.id=a.node_id AND n.inbound_id=a.inbound_id WHERE a.subscriber_id=u.id AND a.node_id=m.node_id)
    OR EXISTS (SELECT 1 FROM subscriber_node_revocations r WHERE r.subscriber_id=u.id AND r.node_id=m.node_id)
 ));
DELETE FROM subscriber_node_grants
WHERE EXISTS (SELECT 1 FROM subscriber_group_grants g JOIN node_group_members m ON m.group_id=g.group_id WHERE g.subscriber_id=subscriber_node_grants.subscriber_id AND m.node_id=subscriber_node_grants.node_id)
 AND NOT EXISTS (SELECT 1 FROM node_group_members m JOIN node_groups ng ON ng.id=m.group_id WHERE m.node_id=subscriber_node_grants.node_id AND NOT EXISTS (SELECT 1 FROM subscriber_group_grants g WHERE g.subscriber_id=subscriber_node_grants.subscriber_id AND g.group_id=m.group_id));
