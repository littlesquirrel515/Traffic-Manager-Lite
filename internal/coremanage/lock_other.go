//go:build !linux && !windows

package coremanage

import (
	"fmt"
)

func AcquireHostLock(path string) (func(), error) {
	return nil, fmt.Errorf("host agent supported on Linux/Windows only")
}
