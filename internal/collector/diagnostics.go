package collector

import (
	"context"
	"traffic-manager-lite/internal/coremonitor"
)

func (s *Scheduler) Diagnose(ctx context.Context, id int64, trigger string) (coremonitor.Report, error) {
	s.mu.Lock()
	if s.busy[id] {
		s.mu.Unlock()
		return coremonitor.Report{}, ErrBusy
	}
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return coremonitor.Report{}, s.ctx.Err()
	}
	s.busy[id] = true
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.busy, id); s.mu.Unlock(); s.wg.Done() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	service := coremonitor.Service{Store: s.Store, Config: s.Config}
	instance, e := service.Instance(ctx, id)
	if e != nil {
		return coremonitor.Report{}, e
	}
	return service.Observe(ctx, instance, true, trigger)
}

func (s *Scheduler) QueueDiagnosis(id int64, trigger string) {
	go func() { _, _ = s.Diagnose(s.ctx, id, trigger) }()
}
