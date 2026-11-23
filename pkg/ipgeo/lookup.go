// Package ipgeo discovers the public egress address and looks up IP geography.
// It is independent of TML, system metrics, and application storage.
package ipgeo

import (
	"errors"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

type Location struct {
	CountryCode string `json:"country_code,omitempty"`
	Country     string `json:"country,omitempty"`
	Region      string `json:"region,omitempty"`
	City        string `json:"city,omitempty"`
}

type namedPlace struct {
	ISOCode string            `maxminddb:"iso_code"`
	Names   map[string]string `maxminddb:"names"`
}

func (p namedPlace) name() string {
	if name := p.Names["zh-CN"]; name != "" {
		return name
	}
	return p.Names["en"]
}

// Lookup reads a locally managed DB-IP or GeoLite2 Country/City MMDB file.
// A zero Location with no error means the address has no geographic record.
func Lookup(path string, ip netip.Addr) (Location, error) {
	if !ip.IsValid() || ip.Zone() != "" {
		return Location{}, errors.New("invalid IP address")
	}
	reader, err := maxminddb.Open(path)
	if err != nil {
		return Location{}, err
	}
	defer reader.Close()
	return lookup(reader, ip)
}

func lookup(reader *maxminddb.Reader, ip netip.Addr) (Location, error) {
	result := reader.Lookup(ip.Unmap())
	if err := result.Err(); err != nil || !result.Found() {
		return Location{}, err
	}
	var record struct {
		Country      namedPlace   `maxminddb:"country"`
		City         namedPlace   `maxminddb:"city"`
		Subdivisions []namedPlace `maxminddb:"subdivisions"`
	}
	if err := result.Decode(&record); err != nil {
		return Location{}, err
	}
	location := Location{CountryCode: record.Country.ISOCode, Country: record.Country.name(), City: record.City.name()}
	if len(record.Subdivisions) > 0 {
		location.Region = record.Subdivisions[0].name()
	}
	return location, nil
}
