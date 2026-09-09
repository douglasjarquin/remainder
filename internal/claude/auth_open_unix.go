//go:build unix

package claude

import (
	"errors"
	"os"
	"syscall"
)

func openAuthFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("Claude authentication file cannot be read")
	}
	return os.NewFile(uintptr(fd), path), nil
}
