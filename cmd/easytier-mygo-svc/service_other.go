//go:build !windows

package main

import "context"

// runService is only for the Windows service manager.
func runService(func(ctx context.Context) error) (bool, error) { return false, nil }
