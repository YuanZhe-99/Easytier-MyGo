package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
)

// RunService is the executable's initialization-and-run boundary. Ownership is
// acquired before constructors read or rewrite durable state, and released only
// after the API, recovery jobs and managed processes have stopped. New/Run remain
// available for in-package tests; production entry points must use RunService.
func RunService(ctx context.Context, opts Options, manifestPath string) error {
	if ctx.Err() != nil {
		return nil
	}
	if opts.StateDir == "" {
		return errors.New("daemon: StateDir is required")
	}
	if err := opts.Logging.Validate(); err != nil {
		return err
	}
	var ids []string
	var manifest networkManifest
	if manifestPath != "" {
		if opts.MonitorOnly || opts.ConfigFile != "" {
			return errors.New("network manifest requires managed mode without --config")
		}
		var err error
		manifest, err = readNetworkManifest(manifestPath)
		if err != nil {
			return err
		}
		for _, entry := range manifest.Networks {
			ids = append(ids, entry.ProfileID)
		}
	}
	release, err := lockNetworkState(opts.StateDir, ids)
	if err != nil {
		return err
	}
	defer release()
	if ctx.Err() != nil {
		return nil
	}
	if manifestPath != "" {
		service, err := newNetworks(opts, manifest)
		if err != nil {
			return err
		}
		return service.run(ctx)
	}
	d, err := New(opts)
	if err != nil {
		return err
	}
	return d.run(ctx)
}

func lockNetworkState(root string, ids []string) (func(), error) {
	release, err := lockState(root)
	if err != nil {
		return nil, err
	}
	releases := []func(){release}
	cleanup := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	for _, id := range ids {
		release, err := lockState(filepath.Join(root, "profiles", id))
		if err != nil {
			cleanup()
			return nil, err
		}
		releases = append(releases, release)
	}
	return cleanup, nil
}
