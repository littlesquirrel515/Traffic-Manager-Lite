package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen, DBPath, BackupDir, AdminUser, AdminPassword, Timezone, LogLevel, ConfigRoot string
	Interval, Timeout, ActiveWindow                                                     time.Duration
	Retention                                                                           int
	Archive, SecureCookie                                                               bool
	AllowedTargets                                                                      []string
}

func value(k, d string) string {
	if v := os.Getenv("TML_" + k); v != "" {
		return v
	}
	return d
}
func Load() (Config, error) {
	c := Config{Listen: value("LISTEN", ":8080"), DBPath: value("DB_PATH", "data/traffic.db"), BackupDir: value("BACKUP_DIR", "backups"), AdminUser: value("ADMIN_USER", "admin"), AdminPassword: value("ADMIN_PASSWORD", ""), Timezone: value("TIMEZONE", "Asia/Shanghai"), LogLevel: value("LOG_LEVEL", "info"), ConfigRoot: value("CONFIG_ROOT", "configs"), AllowedTargets: strings.Split(value("ALLOWED_TARGETS", "127.0.0.1,::1"), ",")}
	var err error
	for k, p := range map[string]*time.Duration{"COLLECT_INTERVAL": &c.Interval, "COLLECT_TIMEOUT": &c.Timeout, "ACTIVE_WINDOW": &c.ActiveWindow} {
		d := map[string]string{"COLLECT_INTERVAL": "10s", "COLLECT_TIMEOUT": "8s", "ACTIVE_WINDOW": "60s"}[k]
		*p, err = time.ParseDuration(value(k, d))
		if err != nil || *p < time.Second {
			return c, fmt.Errorf("invalid TML_%s", k)
		}
	}
	c.Retention, err = strconv.Atoi(value("RAW_RETENTION_DAYS", "30"))
	if err != nil || c.Retention != 30 {
		return c, fmt.Errorf("TML_RAW_RETENTION_DAYS must be 30 for V1")
	}
	c.Archive, err = strconv.ParseBool(value("ARCHIVE_ENABLED", "true"))
	if err != nil {
		return c, err
	}
	c.SecureCookie, err = strconv.ParseBool(value("SECURE_COOKIE", "true"))
	if err != nil {
		return c, err
	}
	if len(c.AdminPassword) < 16 {
		return c, fmt.Errorf("TML_ADMIN_PASSWORD must contain at least 16 characters")
	}
	if strings.HasPrefix(c.AdminPassword, "REPLACE_") {
		return c, fmt.Errorf("replace the example admin password before starting")
	}
	if _, err = time.LoadLocation(c.Timezone); err != nil {
		return c, err
	}
	return c, nil
}
