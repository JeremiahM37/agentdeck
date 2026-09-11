//go:build windows

package sessions

import (
	"errors"
	"os"
)

func platformFileIdentity(os.FileInfo) (uint64, uint64, error) {
	return 0, 0, errors.New("database identity is unsupported on Windows")
}
