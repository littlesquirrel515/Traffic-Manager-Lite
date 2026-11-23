// Package systeminfo collects local resource metrics without application,
// database, authentication, or HTTP dependencies.
package systeminfo

import "time"

type DiskPath struct {
	Name string
	Path string
}

type Options struct {
	Disks          []DiskPath
	Files          []DiskPath
	HostNetworkDir string
	HostOSFile     string
	PublicIPs      []string
}

type Issue struct {
	Component string `json:"component"`
	Code      string `json:"code"`
}

type Snapshot struct {
	ObservedAt time.Time  `json:"observed_at"`
	DurationMS int64      `json:"duration_ms"`
	Window     float64    `json:"sample_seconds"`
	System     System     `json:"system"`
	CPU        CPU        `json:"cpu"`
	Memory     *Memory    `json:"memory"`
	Swap       *Swap      `json:"swap"`
	Container  Container  `json:"container"`
	Process    Process    `json:"process"`
	Disks      []Disk     `json:"disks"`
	Files      []File     `json:"files"`
	Interfaces []NIC      `json:"interfaces"`
	DiskIO     []DeviceIO `json:"disk_io"`
	Pressure   *Pressure  `json:"pressure"`
	HostIPs    []Address  `json:"host_ips"`
	PublicIPs  []string   `json:"declared_public_ips"`
	Issues     []Issue    `json:"issues"`
}

type System struct {
	Hostname      string  `json:"hostname"`
	OS            string  `json:"os"`
	Architecture  string  `json:"architecture"`
	Platform      string  `json:"runtime_platform"`
	HostPlatform  string  `json:"host_platform"`
	Kernel        string  `json:"kernel"`
	Uptime        *uint64 `json:"kernel_uptime_seconds"`
	Containerized bool    `json:"containerized"`
	NetworkScope  string  `json:"network_scope"`
}

type CPU struct {
	Model         string   `json:"model"`
	Logical       int      `json:"logical"`
	Physical      int      `json:"physical"`
	MHz           float64  `json:"mhz"`
	Percent       *float64 `json:"percent"`
	IOWaitPercent *float64 `json:"iowait_percent"`
	StealPercent  *float64 `json:"steal_percent"`
	Load1         *float64 `json:"load1"`
	Load5         *float64 `json:"load5"`
	Load15        *float64 `json:"load15"`
}

type Memory struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	Free      uint64  `json:"free"`
	Cache     uint64  `json:"cache"`
	Percent   float64 `json:"percent"`
}

type SwapDevice struct {
	Name string `json:"name"`
	Used uint64 `json:"used"`
	Free uint64 `json:"free"`
}

type Swap struct {
	Total   uint64       `json:"total"`
	Used    uint64       `json:"used"`
	Free    uint64       `json:"free"`
	Percent float64      `json:"percent"`
	Devices []SwapDevice `json:"devices"`
}

type Container struct {
	Status             string   `json:"status"`
	Version            int      `json:"version"`
	MemoryCurrent      *uint64  `json:"memory_current"`
	MemoryWorkingSet   *uint64  `json:"memory_working_set"`
	MemoryLimit        *uint64  `json:"memory_limit"`
	MemoryLimitState   string   `json:"memory_limit_state"`
	SwapCurrent        *uint64  `json:"swap_current"`
	SwapLimit          *uint64  `json:"swap_limit"`
	CPUCores           *float64 `json:"effective_cpu_cores"`
	CPUQuotaCores      *float64 `json:"cpu_quota_cores"`
	CPUPercent         *float64 `json:"cpu_single_core_percent"`
	CPUCapacityPercent *float64 `json:"cpu_capacity_percent"`
	ThrottleCount      *uint64  `json:"throttle_count"`
	ThrottleDelta      *uint64  `json:"throttle_delta"`
	OOMKills           *uint64  `json:"oom_kills"`
}

type Process struct {
	PID                 int32    `json:"pid"`
	RSS                 *uint64  `json:"rss"`
	VirtualMemory       *uint64  `json:"virtual_memory"`
	CPUPercent          *float64 `json:"cpu_single_core_percent"`
	CPUCapacityPercent  *float64 `json:"cpu_capacity_percent"`
	Uptime              *uint64  `json:"uptime_seconds"`
	Threads             *int32   `json:"threads"`
	FileDescriptors     *int32   `json:"file_descriptors"`
	FileDescriptorLimit *uint64  `json:"file_descriptor_limit"`
	ReadBytes           *uint64  `json:"read_bytes"`
	WriteBytes          *uint64  `json:"write_bytes"`
	GoVersion           string   `json:"go_version"`
	Goroutines          uint64   `json:"goroutines"`
	GoHeap              uint64   `json:"go_heap"`
	GoRuntimeMemory     uint64   `json:"go_runtime_memory"`
	GCCycles            uint64   `json:"gc_cycles"`
	GOMAXPROCS          int      `json:"gomaxprocs"`
}

type Disk struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	Status      string  `json:"status"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Available   uint64  `json:"available"`
	Percent     float64 `json:"percent"`
	InodesTotal uint64  `json:"inodes_total"`
	InodesFree  uint64  `json:"inodes_free"`
}

type File struct {
	Name   string  `json:"name"`
	Size   *uint64 `json:"size"`
	Status string  `json:"status"`
}

type Address struct {
	IP    string `json:"ip"`
	Scope string `json:"scope"`
}

type NIC struct {
	Name      string     `json:"name"`
	MTU       int        `json:"mtu"`
	Up        bool       `json:"up"`
	Addresses []Address  `json:"addresses"`
	IO        *NetworkIO `json:"io"`
}

type NetworkIO struct {
	Received      uint64   `json:"received_bytes"`
	Sent          uint64   `json:"sent_bytes"`
	ReceiveRate   *float64 `json:"receive_bytes_per_second"`
	SendRate      *float64 `json:"send_bytes_per_second"`
	ReceiveErrors uint64   `json:"receive_errors"`
	SendErrors    uint64   `json:"send_errors"`
	ReceiveDrops  uint64   `json:"receive_drops"`
	SendDrops     uint64   `json:"send_drops"`
}

type DeviceIO struct {
	Name      string   `json:"name"`
	ReadRate  *float64 `json:"read_bytes_per_second"`
	WriteRate *float64 `json:"write_bytes_per_second"`
	ReadIOPS  *float64 `json:"read_iops"`
	WriteIOPS *float64 `json:"write_iops"`
}

type Pressure struct {
	CPU    *float64 `json:"cpu_some_avg10"`
	Memory *float64 `json:"memory_some_avg10"`
	IO     *float64 `json:"io_some_avg10"`
}
