package telemetry

import "syscall"

// expandShortName resolves 8.3 aliases such as RUNNER~1, which name the same
// directory as the long form and are not a redirection a caller could plant.
func expandShortName(path string) string {
	from, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buffer := make([]uint16, syscall.MAX_LONG_PATH)
	n, err := syscall.GetLongPathName(from, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 || int(n) > len(buffer) {
		return path
	}
	return syscall.UTF16ToString(buffer[:n])
}
