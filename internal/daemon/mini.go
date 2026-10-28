package daemon

import (
	"errors"
	"fmt"
	"slices"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/profiles"
)

// errAlreadyMini refuses to copy a network that already runs on mini.
var errAlreadyMini = errors.New("this network already runs on EasyTier mini")

// miniCopy makes a network for EasyTier mini from an existing one: the
// original stays as it is (its backup), and the copy, named
// "<name> (mini)", leaves out what mini would silently ignore.
// Returns: the new network and the feature IDs left out
// (configdoc.StripForMini).
// Side effects: creates a stopped network and its worker.
// Notes: the copy gets a fresh instance_id when the original has one, so
// both can run side by side.
func (d *Daemon) miniCopy(id string) (profiles.Profile, []string, error) {
	p, err := d.store.Get(id)
	if err != nil {
		return profiles.Profile{}, nil, err
	}
	if p.Mini {
		return profiles.Profile{}, nil, errAlreadyMini
	}
	text, _, err := d.store.Config(id)
	if err != nil {
		return profiles.Profile{}, nil, err
	}
	out, removed, err := configdoc.StripForMini(text)
	if err != nil {
		return profiles.Profile{}, nil, fmt.Errorf("this configuration cannot be copied for EasyTier mini: %w", err)
	}
	if v, err := configdoc.Extract(out); err == nil && v.InstanceID != nil {
		if out, err = configdoc.SetString(out, "instance_id", configdoc.NewUUID()); err != nil {
			return profiles.Profile{}, nil, err
		}
	}
	var names []string
	for _, o := range d.store.List() {
		names = append(names, o.Name)
	}
	cp, err := d.store.CreateFor(miniName(p.Name, names), out, true)
	if err != nil {
		return profiles.Profile{}, nil, err
	}
	d.addWorker(cp.ID)
	d.notify()
	return cp, removed, nil
}

// miniName is the name of a network's mini copy: "<name> (mini)", then
// "<name> (mini 2)", … when taken.
func miniName(name string, taken []string) string {
	n := name + " (mini)"
	for i := 2; slices.Contains(taken, n); i++ {
		n = fmt.Sprintf("%s (mini %d)", name, i)
	}
	return n
}
