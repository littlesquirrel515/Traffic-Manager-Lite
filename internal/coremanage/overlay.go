package coremanage

import (
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
	"traffic-manager-lite/internal/inboundconfig"
)

func publicFor(kind string, b []byte) ([]map[string]any, error) {
	if kind == "" {
		kind = "singbox"
	}
	sources, e := inboundconfig.Sources(kind, b)
	if e != nil {
		return nil, e
	}
	clients, e := inboundconfig.Parse(kind, b)
	if e != nil && kind != "hysteria2" {
		return nil, e
	}
	out := []map[string]any{}
	for _, s := range sources {
		users := []map[string]any{}
		for _, c := range clients {
			if c.Tag == s.Tag {
				users = append(users, map[string]any{"name": c.Profile.UserKey, "has_uuid": c.Profile.UUID != "", "has_password": c.Profile.Password != ""})
			}
		}
		out = append(out, map[string]any{"tag": s.Tag, "protocol": s.Profile.Protocol, "user_count": len(users), "users": users, "authentication_type": s.AuthenticationType})
	}
	return out, nil
}

type ManagedUser struct {
	Inbound  string `json:"inbound"`
	User     User   `json:"user"`
	Previous *User  `json:"previous,omitempty"`
	Original *User  `json:"original,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// MergeOverlay changes only explicitly owned users. All non-user settings and
// external users are retained. Conflicting external credentials stop the plan.
func MergeOverlay(kind string, b []byte, overlay []ManagedUser) ([]byte, error) {
	var root map[string]any
	var e error
	if kind == "hysteria2" {
		e = yaml.Unmarshal(b, &root)
	} else {
		e = json.Unmarshal(b, &root)
	}
	if e != nil || root == nil {
		return nil, errors.New("invalid base configuration")
	}
	seen := map[string]bool{}
	for _, m := range overlay {
		if m.Inbound == "" || m.User.Name == "" || len(m.User.Name) > 200 || strings.ContainsAny(m.User.Name+m.Inbound, "\r\n\x00") {
			return nil, errors.New("invalid managed identity")
		}
		key := m.Inbound + "\x00" + m.User.Name
		if seen[key] {
			return nil, errors.New("duplicate desired user")
		}
		seen[key] = true
		if kind == "hysteria2" {
			auth, _ := root["auth"].(map[string]any)
			if auth == nil || auth["type"] != "userpass" || m.Inbound != "hysteria2" {
				return nil, errors.New("Hysteria2 static overlay requires userpass authentication")
			}
			users, _ := auth["userpass"].(map[string]any)
			if users == nil {
				users = map[string]any{}
				auth["userpass"] = users
			}
			name := strings.ToLower(m.User.Name)
			password := strings.TrimPrefix(m.User.Password, name+":")
			existing, exists := users[name]
			allowed := !exists || existing == password || m.Previous != nil && existing == strings.TrimPrefix(m.Previous.Password, name+":") || m.Original != nil && existing == strings.TrimPrefix(m.Original.Password, name+":")
			if !allowed {
				return nil, errors.New("external credential conflict")
			}
			if m.Enabled {
				if password == "" {
					return nil, errors.New("missing managed password")
				}
				users[name] = password
			} else {
				delete(users, name)
			}
			continue
		}
		inbounds, _ := root["inbounds"].([]any)
		var inbound map[string]any
		count := 0
		for _, v := range inbounds {
			in, _ := v.(map[string]any)
			if in["tag"] == m.Inbound {
				inbound = in
				count++
			}
		}
		if count != 1 {
			return nil, errors.New("target inbound missing or ambiguous")
		}
		protocol, _ := inbound["type"].(string)
		field, namefield, uuidfield := "users", "name", "uuid"
		container := inbound
		if kind == "xray" || kind == "v2fly" {
			protocol, _ = inbound["protocol"].(string)
			field, namefield, uuidfield = "clients", "email", "id"
			container, _ = inbound["settings"].(map[string]any)
			if container == nil {
				container = map[string]any{}
				inbound["settings"] = container
			}
		}
		switch protocol {
		case "vless", "vmess", "trojan", "anytls", "hysteria2":
		default:
			return nil, fmt.Errorf("managed overlay unsupported for protocol %s", protocol)
		}
		users, _ := container[field].([]any)
		matches := 0
		index := -1
		var current map[string]any
		for i, v := range users {
			u, _ := v.(map[string]any)
			if u[namefield] == m.User.Name {
				matches++
				index = i
				current = u
			}
		}
		if matches > 1 {
			return nil, errors.New("duplicate configured user")
		}
		equal := func(u User) bool {
			if protocol == "vless" || protocol == "vmess" {
				return current[uuidfield] == u.UUID
			}
			return current["password"] == u.Password
		}
		if matches == 1 && !equal(m.User) && (m.Previous == nil || !equal(*m.Previous)) && (m.Original == nil || !equal(*m.Original)) {
			return nil, errors.New("external credential conflict")
		}
		if !m.Enabled {
			if index >= 0 {
				users = append(users[:index], users[index+1:]...)
			}
			container[field] = users
			continue
		}
		if current == nil {
			current = map[string]any{namefield: m.User.Name}
		}
		if protocol == "vless" || protocol == "vmess" {
			if !validUUID(m.User.UUID) {
				return nil, errors.New("invalid managed UUID")
			}
			current[uuidfield] = m.User.UUID
		} else {
			if m.User.Password == "" {
				return nil, errors.New("missing managed password")
			}
			current["password"] = m.User.Password
		}
		if protocol == "vless" {
			if m.User.Flow == "" {
				delete(current, "flow")
			} else {
				current["flow"] = m.User.Flow
			}
		}
		if index < 0 {
			users = append(users, current)
		} else {
			users[index] = current
		}
		container[field] = users
	}
	if kind == "hysteria2" {
		return yaml.Marshal(root)
	}
	return json.MarshalIndent(root, "", "  ")
}
