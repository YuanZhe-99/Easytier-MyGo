//go:build !linux

package platformservice

// Windows (SCM) and macOS (launchd) backends are planned in the research
// report; until they exist these calls fail loudly instead of pretending.

const driverSupported = false

func driverInstall(s Spec) error { return ErrUnsupported }

func driverUninstall(s Spec) error { return ErrUnsupported }

func driverStatus(s Spec) (Status, error) { return Status{}, ErrUnsupported }

// LingerEnabled is a systemd concept.
func LingerEnabled() (bool, error) { return false, ErrUnsupported }
