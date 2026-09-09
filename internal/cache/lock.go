package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

type processLock struct {
	file *os.File
}

func acquireLock(ctx context.Context, path string, wait time.Duration) (*processLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cache refresh lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("secure cache refresh lock: %w", err)
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &processLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			return nil, fmt.Errorf("lock cache refresh ownership: %w", err)
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			timer.Stop()
			file.Close()
			return nil, ErrLockTimeout
		case <-timer.C:
		}
	}
}

func (l *processLock) Close() error {
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	return errors.Join(unlockErr, closeErr)
}
