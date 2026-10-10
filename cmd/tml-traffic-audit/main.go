package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
	"traffic-manager-lite/internal/storage"
)

func main() {
	db := flag.String("db", "", "existing database (stop TML before repair)")
	zone := flag.String("timezone", "Asia/Shanghai", "stored database timezone")
	id := flag.Int64("instance", 0, "reviewed Hysteria2 instance ID")
	before := flag.String("before", "", "exclusive RFC3339 cutoff before corrected traffic")
	repair := flag.Bool("repair", false, "apply once; default is dry run")
	confirm := flag.Bool("confirm-legacy-tml", false, "attest selected history exclusively used the audited legacy TML adapter")
	evidence := flag.String("evidence", "", "operator source provenance/reference, no credentials")
	backups := flag.String("backup-dir", "backups", "private automatic backup directory")
	flag.Parse()
	if e := run(*db, *zone, *id, *before, *repair, *confirm, *evidence, *backups); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(db, zone string, id int64, before string, repair, confirm bool, evidence, backup string) error {
	if id <= 0 || db == "" {
		return fmt.Errorf("database and instance required")
	}
	if _, e := os.Stat(db); e != nil {
		return e
	}
	cutoff, e := time.Parse(time.RFC3339, before)
	if e != nil {
		return fmt.Errorf("explicit cutoff required")
	}
	var s *storage.Store
	if repair {
		s, e = storage.Open(db, zone)
	} else {
		s, e = storage.OpenReadOnly(db, zone)
	}
	if e != nil {
		return e
	}
	defer s.Close()
	p, e := s.DirectionPlan(context.Background(), id, cutoff)
	if e != nil {
		return e
	}
	if repair {
		if !confirm || evidence == "" {
			return fmt.Errorf("historical source must be confirmed; unknown data cannot be repaired")
		}
		p, e = s.RepairHysteriaDirection(context.Background(), id, cutoff, evidence, backup)
		if e != nil {
			return e
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"dry_run": !repair, "plan": p, "warning": "未确认的导入/混合历史不得交换；修复仅选择此 HY 实例；月统计由 daily 计算"})
}
