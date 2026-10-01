//go:build !windows

package telemetry

func expandShortName(path string) string { return path }
