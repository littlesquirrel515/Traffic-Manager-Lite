package coremanage

import (
	"fmt"
	"os"
)

func AcquireHostLock(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		return nil, fmt.Errorf("agent lock exists; verify no running agent before removing stale lock")
	}
	return func() { f.Close(); os.Remove(path) }, nil
}
