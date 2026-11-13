package storage

import (
	"context"
	"encoding/json"
	"strings"
)

type TrafficSubscriberName struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Historical   bool   `json:"historical"`
	SharedNative bool   `json:"shared_native"`
}
type TrafficIdentityRef struct {
	InstanceID   int64  `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	InboundTag   string `json:"inbound_tag"`
	CoreUserKey  string `json:"core_user_key"`
}

// TrafficUsers projects saved ownership for display. It never changes users,
// identity grouping, authorization or any traffic/charging source.
func (s *Store) TrafficUsers(ctx context.Context, search string) ([]map[string]any, error) {
	rows, e := s.Rows(ctx, `WITH links AS (
 SELECT c.instance_id,c.core_client_ref client_ref,b.inbound_id,s.id subscriber_id,s.name,b.enabled active,(b.usage_mode='shared_native') shared
 FROM subscriber_credentials b JOIN credential_material c ON c.id=b.credential_id JOIN subscribers s ON s.id=b.subscriber_id
 UNION ALL
 SELECT m.instance_id,m.client_ref,m.inbound_id,s.id,s.name,m.active,0
 FROM subscriber_usage_sources m JOIN subscribers s ON s.id=m.subscriber_id
), matched AS (
 SELECT DISTINCT i.user_id,i.id identity_id,l.subscriber_id,l.name,l.active,l.shared
 FROM identities i JOIN instances x ON x.id=i.instance_id JOIN links l ON l.instance_id=i.instance_id AND l.client_ref=i.core_user_key
 WHERE i.scope='user' AND (
  (i.inbound_tag='' AND x.core_type IN ('xray','v2fly','hysteria2'))
  OR (i.inbound_tag<>'' AND l.inbound_id<>'' AND EXISTS(
   SELECT 1 FROM catalog_inbounds b WHERE b.id=l.inbound_id AND b.instance_id=i.instance_id AND (b.tag=i.inbound_tag OR EXISTS(SELECT 1 FROM inbound_identity_aliases a WHERE a.inbound_id=b.id AND a.instance_id=i.instance_id AND a.tag=i.inbound_tag))
  ))
 )
), preferred AS (
 SELECT m.* FROM matched m WHERE m.active=1 OR NOT EXISTS(SELECT 1 FROM matched n WHERE n.identity_id=m.identity_id AND n.active=1)
), owners AS (
 SELECT user_id,subscriber_id,name,MAX(active) active,MAX(shared) shared FROM preferred GROUP BY user_id,subscriber_id,name
)
SELECT u.*, (SELECT MAX(last_active_at) FROM identities WHERE user_id=u.id) last_active_at,(SELECT json_group_array(json_object('instance_id',i.instance_id,'inbound',i.inbound_tag,'state',COALESCE(p.capability_status,st.stats_state),'checked_at',COALESCE(p.collected_at,st.checked_at),'provider',p.provider_type,'coverage',p.coverage)) FROM identities i JOIN (SELECT instance_id,email,stats_state,checked_at FROM xray_user_states UNION ALL SELECT instance_id,email,stats_state,checked_at FROM core_user_states) st ON st.instance_id=i.instance_id AND st.email=i.core_user_key LEFT JOIN traffic_provenance p ON p.identity_id=i.id WHERE i.user_id=u.id AND i.scope='user') stats_states,(SELECT json_group_array(json_object('id',subscriber_id,'name',name,'historical',json(CASE WHEN active=0 THEN 'true' ELSE 'false' END),'shared_native',json(CASE WHEN shared=1 THEN 'true' ELSE 'false' END))) FROM (SELECT * FROM owners WHERE user_id=u.id ORDER BY name,subscriber_id)) subscriber_owners,
(SELECT json_group_array(json_object('instance_id',i.instance_id,'instance_name',x.name,'inbound_tag',i.inbound_tag,'core_user_key',i.core_user_key)) FROM identities i JOIN instances x ON x.id=i.instance_id WHERE i.user_id=u.id AND i.scope='user') identity_refs,
(SELECT count(DISTINCT identity_id) FROM preferred WHERE user_id=u.id) matched_identities FROM users u ORDER BY display_name`)
	if e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for _, u := range rows {
		owners := []TrafficSubscriberName{}
		refs := []TrafficIdentityRef{}
		if e = json.Unmarshal([]byte(u["subscriber_owners"].(string)), &owners); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(u["identity_refs"].(string)), &refs); e != nil {
			return nil, e
		}
		original := u["display_name"].(string)
		alias := len(refs) > 0
		for _, ref := range refs {
			if original == ref.CoreUserKey {
				alias = false
			}
		}
		display, source := original, "core"
		names := []string{}
		for _, owner := range owners {
			names = append(names, owner.Name)
		}
		if alias {
			source = "alias"
		} else if len(names) > 0 {
			display = strings.Join(names, " / ")
			source = "subscriber"
		}
		u["operator_name"] = display
		u["name_source"] = source
		u["subscriber_owners"] = owners
		u["identity_refs"] = refs
		u["partial_ownership"] = len(owners) > 0 && u["matched_identities"].(int64) < int64(len(refs))
		searchParts := []string{display, original, strings.Join(names, " ")}
		for _, ref := range refs {
			searchParts = append(searchParts, ref.InstanceName, ref.InboundTag, ref.CoreUserKey)
		}
		if search == "" || strings.Contains(strings.ToLower(strings.Join(searchParts, " ")), strings.ToLower(strings.TrimSpace(search))) {
			out = append(out, u)
		}
	}
	return out, nil
}
