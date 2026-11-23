//go:build !linux

package systeminfo

func systemPressure() *Pressure    { return nil }
func fileDescriptorLimit() *uint64 { return nil }
