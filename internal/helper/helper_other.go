//go:build !linux

package helper

// Install is not implemented on this platform yet.
func Install(InstallOptions) error { return ErrUnsupported }

// Uninstall is not implemented on this platform yet.
func Uninstall(bool) error { return ErrUnsupported }

// SetBackground is not implemented on this platform yet.
func SetBackground(bool) error { return ErrUnsupported }

// Inspect reports no helper on this platform.
func Inspect() Status { return Status{} }
