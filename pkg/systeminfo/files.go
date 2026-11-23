package systeminfo

import (
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func readSmallFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if len(data) > 1<<20 {
		return "", errors.New("metric file too large")
	}
	return string(data), err
}

func hostPlatform(path string) (string, error) {
	text, err := readSmallFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(text, "\n") {
		if value, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			value = strings.Trim(value, "\"'")
			if len(value) > 256 {
				return "", errors.New("invalid platform name")
			}
			return value, nil
		}
	}
	return "", errors.New("platform name unavailable")
}

func hostAddresses(dir string) ([]Address, error) {
	addresses := map[string]Address{}
	ipv4, firstErr := readSmallFile(filepath.Join(dir, "fib_trie"))
	previous := ""
	for _, line := range strings.Split(ipv4, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "/32" && fields[1] == "host" && fields[2] == "LOCAL" {
			if address, err := netip.ParseAddr(previous); err == nil && !address.IsLoopback() && !address.IsUnspecified() {
				addresses[address.String()] = classifyAddress(address)
			}
		}
		if len(fields) > 0 {
			previous = fields[len(fields)-1]
		}
	}
	ipv6, secondErr := readSmallFile(filepath.Join(dir, "if_inet6"))
	for _, line := range strings.Split(ipv6, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 6 {
			continue
		}
		data, err := hex.DecodeString(fields[0])
		if err != nil || len(data) != 16 {
			continue
		}
		address, ok := netip.AddrFromSlice(data)
		if !ok || address.IsLoopback() || address.IsUnspecified() {
			continue
		}
		addresses[address.String()] = classifyAddress(address)
	}
	keys := make([]string, 0, len(addresses))
	for key := range addresses {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []Address{}
	for _, key := range keys {
		out = append(out, addresses[key])
	}
	return out, errors.Join(firstErr, secondErr)
}

func uintFile(path string) *uint64 {
	text, err := readSmallFile(path)
	if err != nil {
		return nil
	}
	number, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return nil
	}
	return &number
}

func metricFields(path string) map[string]uint64 {
	text, err := readSmallFile(path)
	out := map[string]uint64{}
	if err != nil {
		return out
	}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if number, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
			out[fields[0]] = number
		}
	}
	return out
}
