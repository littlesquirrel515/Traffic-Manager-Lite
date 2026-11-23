# systeminfo

Independent, read-only, on-demand OS/process/cgroup collector. No database, HTTP,
authentication, shell commands or outbound network requests.

```go
c := systeminfo.New(systeminfo.Options{
    Disks: []systeminfo.DiskPath{{Name: "data", Path: "/srv/data"}},
})
snapshot := c.Snapshot(ctx)
```

Keep one Collector per monitored process. Calls share a five-second cache; CPU
rates require two samples and use their actual elapsed interval. A missing metric
is null/unavailable, not zero. Callers should treat returned snapshots as immutable.
Linux supports cgroup v1/v2; other operating systems retain the metrics supported
by gopsutil. Container kernel-visible system metrics and resource-group limits are
reported separately. Optional host metadata paths and public IP declarations must
be supplied by trusted operator configuration.

See [metric definitions and deployment](../../docs/system-information-v2.6.md).
