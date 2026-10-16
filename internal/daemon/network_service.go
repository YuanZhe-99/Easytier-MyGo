package daemon

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/sdnotify"
)

type networkManifest struct {
	Version  int                 `json:"version"`
	Networks []networkDefinition `json:"networks"`
}

type networkDefinition struct {
	Logging   *LoggingOptions `json:"logging,omitempty"`
	ProfileID string          `json:"profile_id"`
	Name      string          `json:"name,omitempty"`
	Config    string          `json:"config"`
	RPC       string          `json:"rpc"`
}

// readNetworkManifest validates all definitions before creating worker state.
func readNetworkManifest(path string) (networkManifest, error) {
	var manifest networkManifest
	if !filepath.IsAbs(path) {
		return manifest, errors.New("network manifest path must be absolute")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if len(b) > 64<<10 {
		return manifest, errors.New("network manifest exceeds 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, errors.New("invalid network manifest JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return manifest, errors.New("network manifest must contain one JSON document")
	}
	if manifest.Version != 1 || len(manifest.Networks) < 1 || len(manifest.Networks) > 4 {
		return manifest, errors.New("network manifest requires version 1 and one to four networks")
	}
	ids, rpcs := map[string]bool{}, map[netip.AddrPort]bool{}
	for _, definition := range manifest.Networks {
		if definition.Logging != nil {
			if err := definition.Logging.Validate(); err != nil {
				return manifest, err
			}
		}
		if definition.Name != "" {
			if err := configdoc.ValidName(definition.Name); err != nil {
				return manifest, errors.New("invalid profile display name")
			}
		}
		id, err := hex.DecodeString(definition.ProfileID)
		if err != nil || len(id) != 16 || hex.EncodeToString(id) != definition.ProfileID || ids[definition.ProfileID] || !filepath.IsAbs(definition.Config) {
			return manifest, errors.New("invalid or duplicate network definition")
		}
		endpoint, err := netip.ParseAddrPort(definition.RPC)
		if err != nil || !endpoint.Addr().IsLoopback() || endpoint.Port() == 0 || rpcs[endpoint] {
			return manifest, errors.New("network RPC must be unique explicit loopback with nonzero port")
		}
		if _, err := configNeedsNoRoot(definition.Config); err != nil {
			return manifest, errors.New("cannot read or parse network config")
		}
		ids[definition.ProfileID] = true
		rpcs[endpoint] = true
	}
	return manifest, nil
}

// NetworkManifestNeedsRoot provides install scope guidance without creating
// state or starting cores. Every entry must be readable and valid.
func NetworkManifestNeedsRoot(path string) (bool, error) {
	manifest, err := readNetworkManifest(path)
	if err != nil {
		return false, err
	}
	needsRoot := false
	for _, definition := range manifest.Networks {
		noRoot, err := configNeedsNoRoot(definition.Config)
		if err != nil {
			return false, err
		}
		needsRoot = needsRoot || !noRoot
	}
	return needsRoot, nil
}

// NewNetworks creates an explicitly configured multi-network manager.
func NewNetworks(opts Options, manifestPath string) (*NetworkService, error) {
	if opts.MonitorOnly || opts.ConfigFile != "" {
		return nil, errors.New("network manifest requires managed mode without --config")
	}
	if opts.StateDir == "" || !filepath.IsAbs(manifestPath) {
		return nil, errors.New("network manifest and state directory are required")
	}
	manifest, err := readNetworkManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	return newNetworks(opts, manifest)
}

func newNetworks(opts Options, manifest networkManifest) (*NetworkService, error) {
	registry, err := loadProfiles(opts.StateDir)
	if err != nil {
		return nil, err
	}
	active := make([]networkDefinition, 0, len(manifest.Networks))
	for i := range manifest.Networks {
		deleted := false
		for _, profile := range registry.Profiles {
			if profile.ID == manifest.Networks[i].ProfileID {
				manifest.Networks[i].Name = profile.Name
				deleted = profile.Deleted
			}
		}
		if !deleted {
			active = append(active, manifest.Networks[i])
		}
	}
	manifest.Networks = active
	if len(active) == 0 {
		return nil, errors.New("network manifest contains no active profiles")
	}
	workers := map[string]*Daemon{}
	// Validate every definition before creating any worker state.
	for _, definition := range manifest.Networks {
		id, err := hex.DecodeString(definition.ProfileID)
		if err != nil || len(id) != 16 || workers[definition.ProfileID] != nil || !filepath.IsAbs(definition.Config) {
			return nil, errors.New("invalid or duplicate network definition")
		}
		if os.Geteuid() != 0 {
			noRoot, err := configNeedsNoRoot(definition.Config)
			if err != nil || !noRoot {
				return nil, errors.New("user network manifest requires no_tun=true; use an explicitly privileged service for TUN")
			}
		}
		workers[definition.ProfileID] = &Daemon{}
	}
	for _, definition := range manifest.Networks {
		workerOpts := opts
		workerOpts.ConfigFile = definition.Config
		workerOpts.RPC = definition.RPC
		if definition.Logging != nil {
			workerOpts.Logging = *definition.Logging
		}
		workerOpts.StateDir = filepath.Join(opts.StateDir, "profiles", definition.ProfileID)
		workerOpts.ConfigDir = filepath.Join(opts.ConfigDir, "profiles", definition.ProfileID)
		if opts.ConfigDir == "" {
			workerOpts.ConfigDir = filepath.Join(workerOpts.StateDir, "networks")
		}
		worker, err := New(workerOpts)
		if err != nil {
			return nil, err
		}
		workers[definition.ProfileID] = worker
	}
	manager, err := newNetworkManager(workers)
	if err != nil {
		return nil, err
	}
	manager.names = make(map[*Daemon]string)
	manager.stateDir = opts.StateDir
	for _, definition := range manifest.Networks {
		manager.names[workers[definition.ProfileID]] = definition.Name
	}
	socket := opts.SocketPath
	if socket == "" {
		socket = localapi.DefaultSocketPath()
	}
	return &NetworkService{manager: manager, socket: socket, stateDir: opts.StateDir}, nil
}

type NetworkService struct {
	manager  *networkManager
	socket   string
	stateDir string
}

func (s *NetworkService) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}
	release, err := lockNetworkState(s.stateDir, s.profileIDs())
	if err != nil {
		return err
	}
	defer release()
	return s.run(ctx)
}

func (s *NetworkService) profileIDs() []string {
	ids := make([]string, 0, len(s.manager.workers))
	for id := range s.manager.workers {
		ids = append(ids, id)
	}
	return ids
}

func (s *NetworkService) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var representative *Daemon
	for _, worker := range s.manager.workers {
		representative = worker
		break
	}
	auth := representative.authorizer()
	ln, err := localapi.ListenWith(localapi.ListenOptions{Path: s.socket, Mode: representative.socketMode(), DirMode: representative.socketDirMode(), Group: representative.opts.SocketGroup, Authorizer: auth})
	if err != nil {
		return err
	}
	defer os.Remove(s.socket)
	if notifier := sdnotify.FromEnv(); notifier != nil {
		s.manager.notifier = notifier
	}
	if s.manager.notifier != nil {
		if err := s.manager.notifier.Ready(); err != nil {
			representative.opts.Logf("network manager ready notification failed: %v", err)
		}
	}
	stoppingDone := make(chan struct{})
	go func() {
		defer close(stoppingDone)
		<-ctx.Done()
		if s.manager.notifier != nil {
			if err := s.manager.notifier.Stopping(); err != nil {
				representative.opts.Logf("network manager stopping notification failed: %v", err)
			}
		}
	}()
	defer func() { cancel(); <-stoppingDone }()
	workerDone := make(chan error, 1)
	serverDone := make(chan error, 1)
	go func() { workerDone <- s.manager.Run(ctx) }()
	go func() { serverDone <- localapi.ServeWith(ctx, ln, s.manager, auth) }()
	var result error
	select {
	case <-ctx.Done():
	case <-s.manager.stop:
	case result = <-serverDone:
		cancel()
		ln.Close()
		<-workerDone
		return result
	case result = <-workerDone:
		cancel()
		ln.Close()
		<-serverDone
		return result
	}
	cancel()
	ln.Close()
	return errors.Join(<-workerDone, <-serverDone)
}
