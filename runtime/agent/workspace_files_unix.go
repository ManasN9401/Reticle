//go:build !windows

package agent

import (
	"os"
	"syscall"
)

func lockWorkspaceFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func replaceWorkspaceFile(from, to string) error { return os.Rename(from, to) }
