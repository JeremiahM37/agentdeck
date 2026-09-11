//go:build !windows

package sessions

import (
	"errors"
	"os"
	"syscall"
)

func platformFileIdentity(info os.FileInfo) (uint64, uint64, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("database identity is unsupported on this platform")
	}
	return uint64(st.Dev), uint64(st.Ino), nil
}
