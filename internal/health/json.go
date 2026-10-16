package health

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The protocol between the daemon and the GUI carries enums as their
// names, not numbers, so snapshots and event logs stay readable and the
// two sides can be inspected by hand.

func (l Level) MarshalJSON() ([]byte, error)   { return json.Marshal(l.String()) }
func (c Cause) MarshalJSON() ([]byte, error)   { return json.Marshal(c.String()) }
func (k Kind) MarshalJSON() ([]byte, error)    { return json.Marshal(k.String()) }
func (d Desired) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }
func (l Lifecycle) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}
func (r RecoveryState) MarshalJSON() ([]byte, error) { return json.Marshal(r.String()) }
func (k EventKind) MarshalJSON() ([]byte, error)     { return json.Marshal(k.String()) }

func (l *Level) UnmarshalJSON(b []byte) error {
	s, err := nameOf(b)
	if err != nil {
		return err
	}
	for _, v := range []Level{LevelUnknown, LevelHealthy, LevelDegraded, LevelFailed} {
		if strings.EqualFold(s, v.String()) {
			*l = v
			return nil
		}
	}
	return fmt.Errorf("unknown health level %q", s)
}

func (c *Cause) UnmarshalJSON(b []byte) error {
	s, err := nameOf(b)
	if err != nil {
		return err
	}
	for _, v := range []Cause{CauseNone, CauseLocalFault, CauseUnresponsive, CauseExternal, CauseCollector, CauseAwaiting} {
		if strings.EqualFold(s, v.String()) {
			*c = v
			return nil
		}
	}
	return fmt.Errorf("unknown cause %q", s)
}

func (k *Kind) UnmarshalJSON(b []byte) error {
	s, err := nameOf(b)
	if err != nil {
		return err
	}
	for _, v := range []Kind{KindService, KindCore, KindInstance, KindProbe} {
		if strings.EqualFold(s, v.String()) {
			*k = v
			return nil
		}
	}
	return fmt.Errorf("unknown component kind %q", s)
}

func (d *Desired) UnmarshalJSON(b []byte) error {
	s, err := nameOf(b)
	if err != nil {
		return err
	}
	switch strings.ToLower(s) {
	case "run":
		*d = DesiredRun
	case "stop":
		*d = DesiredStop
	default:
		return fmt.Errorf("unknown desired state %q", s)
	}
	return nil
}

func (l *Lifecycle) UnmarshalJSON(b []byte) error {
	s, err := nameOf(b)
	if err != nil {
		return err
	}
	for _, v := range []Lifecycle{LifecycleUnknown, LifecycleStarting, LifecycleRunning, LifecycleStopping, LifecycleStopped} {
		if strings.EqualFold(s, v.String()) {
			*l = v
			return nil
		}
	}
	return fmt.Errorf("unknown lifecycle %q", s)
}

func (r *RecoveryState) UnmarshalJSON(b []byte) error {
	s, err := nameOf(b)
	if err != nil {
		return err
	}
	for _, v := range []RecoveryState{RecoveryIdle, RecoveryScheduled, RecoveryRunning, RecoveryExhausted} {
		if strings.EqualFold(s, v.String()) {
			*r = v
			return nil
		}
	}
	return fmt.Errorf("unknown recovery state %q", s)
}

func (k *EventKind) UnmarshalJSON(b []byte) error {
	s, err := nameOf(b)
	if err != nil {
		return err
	}
	for _, v := range []EventKind{EventLevelChanged, EventRecoveryScheduled, EventRecoveryStarted, EventRecoveryFinished, EventRecoveryExhausted, EventConfigApplied, EventConfigStarted, EventConfigFailed, EventConfigRollback} {
		if strings.EqualFold(s, v.String()) {
			*k = v
			return nil
		}
	}
	return fmt.Errorf("unknown event kind %q", s)
}

func nameOf(b []byte) (string, error) {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return "", err
	}
	return s, nil
}
