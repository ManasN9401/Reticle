package telemetry

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestSafePathAcceptsShortNameRoot(t *testing.T) {
	root := t.TempDir()
	from, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, syscall.MAX_LONG_PATH)
	n, err := syscall.GetShortPathName(from, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 {
		t.Skip("8.3 names are unavailable on this volume")
	}
	short := syscall.UTF16ToString(buffer[:n])
	if _, err := SafePath(short, "safe/file.txt"); err != nil {
		t.Fatalf("short-name root rejected (%s for %s): %v", short, filepath.Clean(root), err)
	}
}
