package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"traffic-manager-lite/internal/adapters"
	"traffic-manager-lite/internal/config"
	"traffic-manager-lite/internal/core"
	"traffic-manager-lite/internal/security"
	"traffic-manager-lite/internal/storage"
	"traffic-manager-lite/internal/xraymonitor"
)

var ErrBusy = errors.New("此实例正在采集")

type Scheduler struct {
	Store  *storage.Store
	Config config.Config
	mu     sync.Mutex
	busy   map[int64]bool
	wg     sync.WaitGroup
	ctx    context.Context
}

func New(ctx context.Context, s *storage.Store, c config.Config) *Scheduler {
	return &Scheduler{Store: s, Config: c, busy: map[int64]bool{}, ctx: ctx}
}
func (s *Scheduler) Collect(ctx context.Context, id int64) error {
	s.mu.Lock()
	if s.busy[id] {
		s.mu.Unlock()
		return ErrBusy
	}
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return s.ctx.Err()
	}
	s.busy[id] = true
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.busy, id); s.mu.Unlock(); s.wg.Done() }()
	requestContext, requestCancel := context.WithCancel(ctx)
	defer requestCancel()
	ctx, cancel := context.WithTimeout(requestContext, s.Config.Timeout)
	stop := context.AfterFunc(s.ctx, requestCancel)
	defer stop()
	defer cancel()
	list, e := s.Store.Instances(ctx)
	if e != nil {
		return e
	}
	var inst *core.Instance
	for _, i := range list {
		if i.ID == id {
			v := i
			inst = &v
			break
		}
	}
	if inst == nil {
		return fmt.Errorf("instance not found")
	}
	if !inst.Enabled {
		return fmt.Errorf("instance disabled")
	}
	if !inst.ServerEnabled {
		return fmt.Errorf("服务器已停用，所属实例暂停采集")
	}
	if inst.CoreType == "xray" {
		report, err := (xraymonitor.Service{Store: s.Store, Config: s.Config}).Observe(requestContext, *inst, false, "collection")
		if err != nil {
			return s.recordError(ctx, id, err)
		}
		for _, health := range report.Health {
			if health.Status == "Error" {
				return fmt.Errorf("%s: %s", health.Collector, health.Summary)
			}
		}
		return nil
	}
	policy := (security.Policy{Allowed: s.Config.AllowedTargets}).ForEndpoints(inst.APIEndpoint, inst.ControlEndpoint)
	for _, endpoint := range []string{inst.APIEndpoint, inst.ControlEndpoint} {
		if endpoint != "" {
			if e := policy.Check(ctx, security.TargetAddress(endpoint)); e != nil {
				return s.recordError(ctx, id, e)
			}
		}
	}
	a, e := adapters.New(*inst, policy)
	if e != nil {
		return s.recordError(ctx, id, e)
	}
	defer a.Close()
	records, e := a.CollectTraffic(ctx)
	if e == nil {
		e = s.Store.Apply(ctx, id, records)
	}
	if e != nil {
		persist, stop := context.WithTimeout(s.ctx, 2*time.Second)
		defer stop()
		caps, _ := json.Marshal(a.Capabilities())
		_, _ = s.Store.DB.ExecContext(persist, "UPDATE instances SET capabilities_json=? WHERE id=?", string(caps), id)
		return s.recordError(ctx, id, e)
	}
	online, oe := a.CollectOnline(ctx)
	if oe == nil {
		e = s.Store.SaveOnline(ctx, id, online)
		if e != nil {
			return e
		}
	}
	caps, _ := json.Marshal(a.Capabilities())
	persist, finish := context.WithTimeout(s.ctx, 2*time.Second)
	defer finish()
	if versioned, ok := a.(interface{ DetectedVersion() string }); ok {
		_, _ = s.Store.DB.ExecContext(persist, "UPDATE instances SET detected_version=? WHERE id=?", versioned.DetectedVersion(), id)
	}
	errText := ""
	if oe != nil && !errors.Is(oe, core.ErrUnsupported) {
		errText = "在线指标采集失败，保留上次快照"
	}
	_, e = s.Store.DB.ExecContext(persist, "UPDATE instances SET last_collected_at=?,last_error=?,capabilities_json=? WHERE id=?", storage.Stamp(time.Now()), errText, string(caps), id)
	return e
}
func (s *Scheduler) recordError(ctx context.Context, id int64, e error) error {
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	// Adapter errors are sanitized; never persist server response bodies or credentials.
	msg := e.Error()
	if len(msg) > 250 {
		msg = "采集失败"
	}
	_, _ = s.Store.DB.ExecContext(ctx, "UPDATE instances SET last_error=? WHERE id=?", msg, id)
	_, _ = s.Store.DB.ExecContext(ctx, "INSERT INTO collector_errors(instance_id,message,created_at) VALUES(?,?,?)", id, msg, storage.Stamp(time.Now()))
	slog.Warn("collection failed", "instance_id", id, "error", msg)
	return e
}
func (s *Scheduler) Run() {
	ticker := time.NewTicker(s.Config.Interval)
	defer ticker.Stop()
	archive := time.NewTicker(24 * time.Hour)
	defer archive.Stop()
	collect := func() {
		list, e := s.Store.Instances(s.ctx)
		if e != nil {
			slog.Error("instance list failed")
			return
		}
		for _, i := range list {
			if i.Enabled && i.ServerEnabled {
				go func(id int64) { _ = s.Collect(s.ctx, id) }(i.ID)
			}
		}
	}
	if s.Config.Archive {
		if n, e := s.Store.Archive(s.ctx, time.Now()); e != nil {
			slog.Error("startup archive failed", "error", e)
		} else {
			slog.Info("startup archive complete", "deleted", n)
		}
	}
	collect()
	for {
		select {
		case <-s.ctx.Done():
			s.mu.Lock()
			s.mu.Unlock()
			s.wg.Wait()
			return
		case <-ticker.C:
			collect()
		case <-archive.C:
			if s.Config.Archive {
				n, e := s.Store.Archive(s.ctx, time.Now())
				if e != nil {
					slog.Error("archive failed", "error", e)
				} else {
					slog.Info("archive complete", "deleted", n)
				}
			}
		}
	}
}
