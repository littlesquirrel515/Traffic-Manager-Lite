//go:build !linux && !windows

package coremanage

func preserveOwner(original, candidate string) error { return nil }
