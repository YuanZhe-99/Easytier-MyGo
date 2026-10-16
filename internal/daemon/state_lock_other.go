//go:build !linux

package daemon

// Linux owns the implemented service lifecycle; other backends remain deferred.
func lockState(string) (func(), error) { return func() {}, nil }
