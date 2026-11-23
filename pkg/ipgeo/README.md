# ipgeo

Import this package from a Go application or copy it to another module. It has
no dependency on TML, its web stack, the metrics collector, or SQLite.

```go
resolver := ipgeo.New(ipgeo.Options{DatabasePath: "/srv/geoip/city.mmdb"})
snapshot := resolver.Current(nil)
location, err := ipgeo.Lookup("/srv/geoip/city.mmdb", netip.MustParseAddr("8.8.8.8"))
```

Keep one resolver per host. `Current` returns immediately and starts an
asynchronous refresh when due. It accepts optional public host IP candidates.
Explicit declarations take priority. If neither is available, fixed public
HTTPS services discover the current egress address. Use `DisableOnline` when
the caller must operate offline. `Lookup` always uses the local MMDB file.

The open-source reader is [maxminddb-golang](https://github.com/oschwald/maxminddb-golang).
Supported data includes [DB-IP Country Lite](https://db-ip.com/db/download/ip-to-country-lite),
[DB-IP City Lite](https://db-ip.com/db/download/ip-to-city-lite), and
[GeoLite2 Country/City](https://dev.maxmind.com/geoip/). Keep the data provider's
attribution and update terms when reusing the package.
