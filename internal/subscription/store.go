package subscription

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

func Token() (string, string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", "", e
	}
	t := base64.RawURLEncoding.EncodeToString(b)
	return t, Hash(t), nil
}
func Hash(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

var ErrNoUsableNodes = errors.New("订阅没有可用节点，请检查节点、凭据和格式兼容性")
