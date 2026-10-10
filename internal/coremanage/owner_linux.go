package coremanage

import (
	"os"
	"syscall"
)

func preserveOwner(original, candidate string) error {
	info, e := os.Stat(original)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	st := info.Sys().(*syscall.Stat_t)
	return os.Chown(candidate, int(st.Uid), int(st.Gid))
}
