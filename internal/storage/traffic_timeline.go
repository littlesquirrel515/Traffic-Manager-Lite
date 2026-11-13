package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type HistoryFilter struct {
	From, To, Grain, Subscriber, Core string
	UserID, InstanceID, ServerID      int64
}

type HistoryBucket struct {
	Label    string `json:"label"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	State    string `json:"state"`
}

type TrafficTimeline struct {
	From     string          `json:"from"`
	To       string          `json:"to"`
	Timezone string          `json:"timezone"`
	Buckets  []HistoryBucket `json:"buckets"`
}

func (s *Store) TrafficTimeline(ctx context.Context, f HistoryFilter, now time.Time) (TrafficTimeline, error) {
	out := TrafficTimeline{Timezone: s.Location.String(), Buckets: []HistoryBucket{}}
	const dateLayout = "2006-01-02"
	if f.From == "" {
		f.From = now.In(s.Location).Format(dateLayout)
	}
	if f.To == "" {
		f.To = f.From
	}
	from, err := time.ParseInLocation(dateLayout, f.From, s.Location)
	if err != nil {
		return out, fmt.Errorf("请输入有效的开始日期")
	}
	to, err := time.ParseInLocation(dateLayout, f.To, s.Location)
	if err != nil || to.Before(from) || to.After(from.AddDate(0, 0, 365)) {
		return out, fmt.Errorf("请选择有效日期，最多查询 366 天")
	}
	out.From, out.To = f.From, f.To
	end := to.AddDate(0, 0, 1)
	var step time.Duration
	switch f.Grain {
	case "day":
	case "hour":
		step = time.Hour
	case "half-hour":
		step = 30 * time.Minute
	default:
		return out, fmt.Errorf("无效的统计粒度")
	}
	if step != 0 && f.From != f.To {
		return out, fmt.Errorf("小时和半小时查询请选择同一天")
	}
	if f.UserID < 0 || f.Subscriber != "" && (f.UserID != 0 || f.Core != "") {
		return out, fmt.Errorf("无效的统计对象")
	}

	// A current exclusive meter selects a set of identities. EXISTS keeps each
	// identity in the series once, regardless of bindings and alias count.
	where := `i.scope='user' AND (?=0 OR i.user_id=?) AND (?='' OR x.core_type=?) AND (?=0 OR i.instance_id=?) AND (?=0 OR x.server_id=?)
	AND (?='' OR EXISTS (
	 SELECT 1 FROM subscriber_usage_sources m
	 WHERE m.subscriber_id=? AND m.active=1 AND m.instance_id=i.instance_id AND m.client_ref=i.core_user_key
	 AND ((m.inbound_id='' AND i.inbound_tag='') OR (m.inbound_id<>'' AND i.inbound_tag<>'' AND EXISTS (
	  SELECT 1 FROM catalog_inbounds b WHERE b.id=m.inbound_id AND b.instance_id=i.instance_id AND
	  (b.tag=i.inbound_tag OR EXISTS (SELECT 1 FROM inbound_identity_aliases a WHERE a.inbound_id=b.id AND a.instance_id=i.instance_id AND a.tag=i.inbound_tag))
	 )))
	 AND NOT EXISTS (
	  SELECT 1 FROM subscriber_credentials sc JOIN credential_material c ON c.id=sc.credential_id
	  WHERE sc.enabled=1 AND sc.usage_mode='shared_native' AND c.instance_id=m.instance_id AND c.core_client_ref=m.client_ref
	  AND (m.inbound_id='' OR sc.inbound_id=m.inbound_id)
	 )
	))`
	filter := []any{f.UserID, f.UserID, f.Core, f.Core, f.InstanceID, f.InstanceID, f.ServerID, f.ServerID, f.Subscriber, f.Subscriber}
	prefix := `WITH selected AS (SELECT i.id FROM identities i JOIN instances x ON x.id=i.instance_id WHERE ` + where + `) `
	var query string
	var args []any
	positions := map[string]int{}
	// UTC hour aggregates are authoritative when the chosen local boundaries
	// align. For other time zones build local hour buckets from retained samples.
	useSamples := f.Grain == "half-hour" || step != 0 && (from.Unix()%3600 != 0 || end.Unix()%3600 != 0)
	if step == 0 {
		query = prefix + `SELECT date bucket, SUM(upload_bytes) upload, SUM(download_bytes) download FROM traffic_daily JOIN selected ON selected.id=identity_id WHERE date BETWEEN ? AND ? GROUP BY date`
		args = append(filter, f.From, f.To)
	} else if !useSamples {
		query = prefix + `SELECT hour_start bucket, SUM(upload_bytes) upload, SUM(download_bytes) download FROM traffic_hourly JOIN selected ON selected.id=identity_id WHERE hour_start>=? AND hour_start<? GROUP BY hour_start`
		args = append(filter, Stamp(from), Stamp(end))
	} else {
		query = prefix + `SELECT CAST((CAST(strftime('%s',collected_at) AS INTEGER)-?)/? AS TEXT) bucket,
		 SUM(upload_delta) upload, SUM(download_delta) download FROM traffic_samples JOIN selected ON selected.id=identity_id
		 WHERE collected_at>=? AND collected_at<? GROUP BY bucket`
		args = append(filter, from.Unix(), int64(step/time.Second), Stamp(from), Stamp(end))
	}
	for current := from; current.Before(end); {
		bucket := HistoryBucket{Label: current.Format(dateLayout), State: "unobserved"}
		key := bucket.Label
		if step != 0 {
			bucket.Label = current.Format("15:04 -07:00")
			key = Stamp(current)
			if useSamples {
				key = fmt.Sprint(len(out.Buckets))
			}
		}
		positions[key] = len(out.Buckets)
		out.Buckets = append(out.Buckets, bucket)
		if step == 0 {
			current = current.AddDate(0, 0, 1)
		} else {
			current = current.Add(step)
		}
	}
	if useSamples {
		// Inspect archives in the same SQLite snapshot as retained samples.
		query += ` UNION ALL SELECT 'archive:'||hour_start, 0, 0 FROM archive_ledger
		 JOIN selected ON selected.id=identity_id WHERE hour_start>=? AND hour_start<? GROUP BY hour_start ORDER BY bucket DESC`
		args = append(args, Stamp(from.UTC().Truncate(time.Hour)), Stamp(end))
	}
	rows, err := s.Rows(ctx, query, args...)
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		key := row["bucket"].(string)
		if index, ok := positions[key]; ok {
			bucket := &out.Buckets[index]
			if bucket.State != "archived" {
				bucket.Upload = row["upload"].(int64)
				bucket.Download = row["download"].(int64)
				bucket.State = "observed"
			}
			continue
		}
		if !strings.HasPrefix(key, "archive:") {
			continue
		}
		hour, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(key, "archive:"))
		if err != nil {
			return out, err
		}
		for index := range out.Buckets {
			start := from.Add(time.Duration(index) * step)
			if start.Before(hour.Add(time.Hour)) && start.Add(step).After(hour) {
				out.Buckets[index].State = "archived"
				out.Buckets[index].Upload, out.Buckets[index].Download = 0, 0
			}
		}
	}
	return out, nil
}
