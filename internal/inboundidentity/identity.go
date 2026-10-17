// Package inboundidentity reconciles configuration identities without touching
// proxy configuration, runtime users or historical traffic facts.
package inboundidentity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strconv"
	"time"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/inboundconfig"
	"traffic-manager-lite/internal/storage"
)

type old struct {
	ID, Tag, Protocol, Source, Fingerprint, Address string
	Profile                                         core.NodeProfile
}

func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
func Fingerprint(p core.NodeProfile) string {
	p.ListenPorts = nil
	p.Name, p.Address, p.Port, p.UUID, p.Password, p.UserKey, p.Enabled = "", "", 0, "", "", "", false
	h := sha256.Sum256([]byte(encode(p)))
	return hex.EncodeToString(h[:])
}

// Sync computes all matches against the same snapshot, never the loop's evolving
// state. Ambiguous observations are staged, not imported as new business objects.
func Sync(ctx context.Context, tx *sql.Tx, instance int64, source, hash string, b []byte, kind string) (map[string]string, error) {
	incoming, e := inboundconfig.Sources(kind, b)
	if e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,tag,protocol,source_ref,structure_hash,listen_address,profile_json FROM catalog_inbounds WHERE instance_id=? AND retired=0 AND id NOT IN(SELECT inbound_id FROM inbound_retirements)", instance)
	if e != nil {
		return nil, e
	}
	previous := []old{}
	for rows.Next() {
		var o old
		var raw string
		if e = rows.Scan(&o.ID, &o.Tag, &o.Protocol, &o.Source, &o.Fingerprint, &o.Address, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &o.Profile); e != nil {
			rows.Close()
			return nil, e
		}
		if o.Fingerprint == "legacy" {
			o.Fingerprint = Fingerprint(o.Profile)
		}
		previous = append(previous, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	tags := map[string]bool{}
	counts := map[string]int{}
	for _, v := range incoming {
		tags[v.Tag] = true
		counts[Fingerprint(v.Profile)+"|"+v.ListenAddress]++
	}
	type decision struct {
		v              inboundconfig.Source
		p              core.NodeProfile
		fp, id, method string
		review         int
		chosen         *old
	}
	plans := []decision{}
	result := map[string]string{}
	retiredTags := map[string]string{}
	rr, err := tx.QueryContext(ctx, "SELECT id,tag,protocol FROM catalog_inbounds WHERE instance_id=? AND retired=1", instance)
	if err != nil {
		return nil, err
	}
	for rr.Next() {
		var id, tag, protocol string
		if err = rr.Scan(&id, &tag, &protocol); err != nil {
			rr.Close()
			return nil, err
		}
		retiredTags[tag+"|"+protocol] = id
	}
	err = rr.Err()
	rr.Close()
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	now := storage.Stamp(time.Now())
	if _, e = tx.ExecContext(ctx, "UPDATE catalog_inbounds SET present=0 WHERE instance_id=?", instance); e != nil {
		return nil, e
	}
	for _, v := range incoming {
		if retiredID := retiredTags[v.Tag+"|"+v.Profile.Protocol]; retiredID != "" {
			result[v.Tag] = retiredID
			continue
		}
		p := v.Profile
		p.Name, p.Address, p.Port = "", "", 0
		fp := Fingerprint(p)
		var chosen *old
		method := "new"
		candidates := []string{}
		// A confirmed manual mapping is scoped to instance/source/tag/protocol.
		var explicit string
		_ = tx.QueryRowContext(ctx, "SELECT resolved_inbound_id FROM inbound_identity_pending WHERE instance_id=? AND source_locator=? AND tag=? AND protocol=? AND config_hash=? AND status='resolved'", instance, source, v.Tag, p.Protocol, hash).Scan(&explicit)
		for i := range previous {
			if previous[i].ID == explicit {
				chosen = &previous[i]
				method = "manual"
			}
		}
		if chosen == nil {
			for i := range previous {
				o := &previous[i]
				if o.Tag == v.Tag && o.Protocol == p.Protocol {
					chosen = o
					method = "current_tag"
					break
				}
			}
			// Exchanged tags/ports are ambiguous even though each tag still exists.
			if chosen != nil && chosen.Fingerprint != fp {
				for _, o := range previous {
					if o.ID != chosen.ID && o.Fingerprint == fp && o.Address == v.ListenAddress && tags[o.Tag] {
						candidates = append(candidates, chosen.ID, o.ID)
						chosen = nil
						break
					}
				}
			}
		}
		if chosen == nil && len(candidates) == 0 {
			exact := []int{}
			for i, o := range previous {
				if !tags[o.Tag] && o.Protocol == p.Protocol && o.Fingerprint == fp && o.Address == v.ListenAddress {
					exact = append(exact, i)
				}
			}
			if len(exact) == 1 && counts[fp+"|"+v.ListenAddress] == 1 {
				chosen = &previous[exact[0]]
				method = "unique_semantic"
			} else if len(exact) > 0 {
				for _, i := range exact {
					candidates = append(candidates, previous[i].ID)
				}
			}
		}
		if chosen == nil && len(candidates) == 0 {
			for _, o := range previous {
				if !tags[o.Tag] && o.Protocol == p.Protocol && o.Profile.Transport == p.Transport || o.Tag == v.Tag && o.Protocol != p.Protocol {
					candidates = append(candidates, o.ID)
				}
			}
		}
		if chosen != nil && used[chosen.ID] {
			candidates = append(candidates, chosen.ID)
			chosen = nil
		}
		if chosen == nil && len(candidates) > 0 {
			_, e = tx.ExecContext(ctx, `INSERT INTO inbound_identity_pending(id,instance_id,source_locator,tag,protocol,profile_json,listen_address,candidates_json,config_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(instance_id,source_locator,tag,protocol) DO UPDATE SET profile_json=excluded.profile_json,listen_address=excluded.listen_address,candidates_json=excluded.candidates_json,config_hash=excluded.config_hash,status='needs_review',resolved_inbound_id=NULL`, uuid.NewString(), instance, source, v.Tag, p.Protocol, encode(p), v.ListenAddress, encode(candidates), hash, now)
			if e != nil {
				return nil, e
			}
			continue
		}
		id := uuid.NewString()
		review := 0
		if chosen != nil {
			id = chosen.ID
			used[id] = true
			if chosen.Fingerprint != fp && method != "manual" {
				review = 1
			}
		}
		plans = append(plans, decision{v, p, fp, id, method, review, chosen})
	}
	// SQL's old unique tag constraint requires coordinated renames. Never move a
	// confirmed identity into another unresolved identity's slot.
	for _, plan := range plans {
		for _, o := range previous {
			if o.Tag == plan.v.Tag && o.Protocol == plan.p.Protocol && o.ID != plan.id && !used[o.ID] {
				return nil, fmt.Errorf("Tag交换涉及未确认身份；请先确认该组全部身份再同步")
			}
		}
	}
	for _, plan := range plans {
		if plan.chosen != nil && plan.chosen.Tag != plan.v.Tag {
			if _, e = tx.ExecContext(ctx, "UPDATE catalog_inbounds SET tag=? WHERE id=?", "__tml_transition_"+plan.id, plan.id); e != nil {
				return nil, e
			}
		}
	}
	for _, plan := range plans {
		v, p, fp, id, method, review, chosen := plan.v, plan.p, plan.fp, plan.id, plan.method, plan.review, plan.chosen
		_, e = tx.ExecContext(ctx, `INSERT INTO catalog_inbounds(id,instance_id,source_ref,tag,protocol,listen_port,profile_json,structure_hash,config_hash,present,discovered_at,listen_address,authentication_type,review) VALUES(?,?,?,?,?,?,?,?,?,1,?,?,?,?) ON CONFLICT(id) DO UPDATE SET source_ref=excluded.source_ref,tag=excluded.tag,protocol=excluded.protocol,listen_port=excluded.listen_port,profile_json=excluded.profile_json,structure_hash=excluded.structure_hash,config_hash=excluded.config_hash,present=1,discovered_at=excluded.discovered_at,listen_address=excluded.listen_address,authentication_type=excluded.authentication_type,review=CASE WHEN ? THEN 0 ELSE MAX(catalog_inbounds.review,excluded.review) END`, id, instance, source, v.Tag, p.Protocol, p.ListenPort, encode(p), fp, hash, now, v.ListenAddress, v.AuthenticationType, review, method == "manual")
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE catalog_inbounds SET listen_ports_json=? WHERE id=?", encode(p.ListenPorts), id); e != nil {
			return nil, e
		}
		if chosen != nil {
			before := chosen.Profile.ListenPorts
			if len(before) == 0 && chosen.Profile.ListenPort > 0 {
				before = []string{strconv.Itoa(chosen.Profile.ListenPort)}
			}
			if encode(before) != encode(p.ListenPorts) || chosen.Address != v.ListenAddress || chosen.Fingerprint != fp || chosen.Protocol != p.Protocol {
				if _, e = tx.ExecContext(ctx, "INSERT INTO endpoint_binding_audit(binding_id,action,summary_json,created_at) SELECT id,'source_changed',?,? FROM inbound_endpoint_bindings WHERE inbound_id=?", encode(map[string]any{"old_listen_ports": before, "new_listen_ports": p.ListenPorts, "config_hash": hash, "reason": "verify_forwarding_and_credentials"}), now, id); e != nil {
					return nil, e
				}
				if _, e = tx.ExecContext(ctx, "UPDATE inbound_endpoint_bindings SET validation_status='needs_validation',revision=revision+1,updated_at=? WHERE inbound_id=?", now, id); e != nil {
					return nil, e
				}
			}
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO inbound_identity_aliases VALUES(?,?,?,?,?,?,?) ON CONFLICT(instance_id,source_locator,tag,protocol) DO UPDATE SET inbound_id=excluded.inbound_id,last_seen_at=excluded.last_seen_at`, instance, source, v.Tag, p.Protocol, id, now, now)
		if e != nil {
			return nil, e
		}
		if chosen != nil && (chosen.Tag != v.Tag || chosen.Source != source || chosen.Fingerprint != fp) {
			_, e = tx.ExecContext(ctx, "INSERT INTO inbound_identity_history(inbound_id,instance_id,old_tag,new_tag,old_source,new_source,config_hash,method,created_at) VALUES(?,?,?,?,?,?,?,?,?)", id, instance, chosen.Tag, v.Tag, chosen.Source, source, hash, method, now)
			if e != nil {
				return nil, e
			}
		}
		if chosen != nil && chosen.Tag != v.Tag {
			if _, e = tx.ExecContext(ctx, `UPDATE credential_material SET inbound_tag=? WHERE id IN (SELECT credential_id FROM inbound_client_keys WHERE inbound_id=?)`, v.Tag, id); e != nil {
				return nil, e
			}
		}
		result[v.Tag] = id
	}
	return result, nil
}

func ClientKey(ctx context.Context, tx *sql.Tx, inbound, ref, fallback string) (string, error) {
	var id string
	e := tx.QueryRowContext(ctx, "SELECT credential_id FROM inbound_client_keys WHERE inbound_id=? AND client_ref=?", inbound, ref).Scan(&id)
	if e == nil {
		return id, nil
	}
	if e != sql.ErrNoRows {
		return "", e
	}
	var owned int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM inbound_client_keys WHERE credential_id=?", fallback).Scan(&owned); e != nil {
		return "", e
	}
	if owned > 0 {
		fallback = uuid.NewString()
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO inbound_client_keys VALUES(?,?,?)", inbound, ref, fallback)
	return fallback, e
}

func Resolve(ctx context.Context, s *storage.Store, pending, target, revision string, create bool) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var instance int64
	var protocol, tag, source, hash, profile, address string
	e = tx.QueryRowContext(ctx, "SELECT instance_id,protocol,tag,source_locator,config_hash,profile_json,listen_address FROM inbound_identity_pending WHERE id=? AND status='needs_review'", pending).Scan(&instance, &protocol, &tag, &source, &hash, &profile, &address)
	if e != nil {
		return e
	}
	if hash != revision {
		return fmt.Errorf("配置修订已变化")
	}
	if create {
		target = uuid.NewString()
		var p core.NodeProfile
		json.Unmarshal([]byte(profile), &p)
		_, e = tx.ExecContext(ctx, "INSERT INTO catalog_inbounds(id,instance_id,source_ref,tag,protocol,listen_port,profile_json,structure_hash,config_hash,present,discovered_at,listen_address) VALUES(?,?,?,?,?,?,?,?,?,0,?,?)", target, instance, source, tag, protocol, p.ListenPort, profile, Fingerprint(p), hash, storage.Stamp(time.Now()), address)
	} else {
		var count int
		e = tx.QueryRowContext(ctx, "SELECT count(*) FROM catalog_inbounds WHERE id=? AND instance_id=? AND protocol=? AND present=0", target, instance, protocol).Scan(&count)
		if e == nil && count != 1 {
			return fmt.Errorf("目标身份不是同实例同协议的缺失入站")
		}
	}
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "UPDATE inbound_identity_pending SET status='resolved',resolved_inbound_id=? WHERE id=?", target, pending)
	if e != nil {
		return e
	}
	return tx.Commit()
}
