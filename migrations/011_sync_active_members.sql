-- Keep the stable slot, endpoint, node, grants and billing history. Only the
-- membership of an inactive slot in its own managed sync group is removed.
DELETE FROM node_group_members
WHERE EXISTS (
 SELECT 1 FROM preferred_sync_slots s JOIN preferred_sync_jobs j ON j.id=s.job_id
 WHERE s.node_id=node_group_members.node_id AND j.group_id=node_group_members.group_id AND s.active=0
);
