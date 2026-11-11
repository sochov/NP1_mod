//go:build !linux

package adapters

func diskUsage(path string) map[string]any { return nil }
