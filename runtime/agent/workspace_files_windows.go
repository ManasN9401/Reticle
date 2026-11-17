//go:build windows

package agent

import (
	"os"
	"syscall"
	"unsafe"
)

var workspaceKernel = syscall.NewLazyDLL("kernel32.dll")
var workspaceLockFile = workspaceKernel.NewProc("LockFileEx")
var workspaceMoveFile = workspaceKernel.NewProc("MoveFileExW")

func lockWorkspaceFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if info, e := f.Stat(); e == nil && info.Size() == 0 {
		if _, e = f.WriteAt([]byte{0}, 0); e != nil {
			f.Close()
			return nil, e
		}
	}
	// Same first-byte range used by the SDK's msvcrt edit locks.
	var overlapped syscall.Overlapped
	ok, _, callErr := workspaceLockFile.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		f.Close()
		return nil, callErr
	}
	return f, nil
}
func replaceWorkspaceFile(from, to string) error {
	src, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	ok, _, callErr := workspaceMoveFile.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), 9)
	if ok == 0 {
		return callErr
	}
	return nil
}
