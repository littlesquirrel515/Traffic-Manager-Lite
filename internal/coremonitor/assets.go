package coremonitor

import (
	"context"
	"traffic-manager-lite/internal/core"
)

func (s Service) assetMatches(i core.Instance, o *observation, inbound, user string) []string {
	tags := []string{}
	if o.clientsOK {
		for _, a := range o.clients {
			if a.Email == user && (inbound == "" || inbound == a.Inbound) {
				tags = append(tags, a.Inbound)
			}
		}
		return tags
	}
	rows, err := s.Store.DB.QueryContext(context.Background(), `SELECT DISTINCT inbound_tag FROM core_clients c WHERE instance_id=? AND email=? AND present=1 AND (source NOT IN ('manual_unverified','hysteria_http_auth') OR EXISTS (SELECT 1 FROM catalog_inbounds b JOIN inbound_client_keys k ON k.inbound_id=b.id JOIN credential_material m ON m.id=k.credential_id WHERE b.instance_id=c.instance_id AND b.tag=c.inbound_tag AND k.client_ref=c.email AND b.retired=0 AND b.present=1 AND m.status='available'))`, i.ID, user)
	if err != nil {
		return tags
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		rows.Scan(&tag)
		if inbound == "" || inbound == tag {
			tags = append(tags, tag)
		}
	}
	return tags
}
func (s Service) knownAsset(i core.Instance, o *observation, inbound, user string) bool {
	return inbound != "" && len(s.assetMatches(i, o, inbound, user)) == 1
}
func (s Service) uniqueUserInbound(i core.Instance, o *observation, user string) (string, bool) {
	tags := s.assetMatches(i, o, "", user)
	if len(tags) != 1 {
		return "", false
	}
	return tags[0], true
}
