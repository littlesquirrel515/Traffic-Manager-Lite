package xraymonitor

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"google.golang.org/protobuf/encoding/protowire"
	"regexp"
	"strings"
)

func (s Service) fingerprintKey(ctx context.Context) ([]byte, error) {
	seed := make([]byte, 32)
	if _, e := rand.Read(seed); e != nil {
		return nil, e
	}
	_, e := s.Store.DB.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('xray_asset_fingerprint_key',?) ON CONFLICT DO NOTHING", hex.EncodeToString(seed))
	if e != nil {
		return nil, e
	}
	var stored string
	if e = s.Store.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='xray_asset_fingerprint_key'").Scan(&stored); e != nil {
		return nil, e
	}
	return hex.DecodeString(stored)
}
func accountStrings(data []byte) map[protowire.Number]string {
	result := map[protowire.Number]string{}
	for len(data) > 0 {
		number, kind, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil
		}
		data = data[n:]
		if kind == protowire.BytesType {
			value, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return nil
			}
			result[number] = string(value)
			data = data[n:]
		} else {
			n := protowire.ConsumeFieldValue(number, kind, data)
			if n < 0 {
				return nil
			}
			data = data[n:]
		}
	}
	return result
}
func fingerprint(key []byte, protocol, credential, flow string) string {
	if protocol == "vless" || protocol == "vmess" {
		if !regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(credential) {
			return ""
		}
		credential = strings.ToLower(credential)
	} else if protocol != "trojan" && protocol != "shadowsocks" {
		return ""
	}
	if credential == "" {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(protocol + "\x00" + credential + "\x00" + flow))
	return hex.EncodeToString(mac.Sum(nil))
}
