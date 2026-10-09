package storage

import "encoding/json"

func decode(s string, v any) { _ = json.Unmarshal([]byte(s), v) }
