//go:build !linux

package systeminfo

func inContainer() bool { return false }

func collectContainer() (Container, *float64) {
	return Container{Status: "unsupported", MemoryLimitState: "unknown"}, nil
}
