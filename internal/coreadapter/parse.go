package coreadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var errNoVersion = errors.New("no version in output")

// instanceResult is the wrapper easytier-cli prints when a query covers
// more than one network instance (easytier-cli.rs print_json_results).
type instanceResult struct {
	InstanceID   string          `json:"instance_id"`
	InstanceName string          `json:"instance_name"`
	Result       json.RawMessage `json:"result"`
}

// unwrapMulti detects the multi-instance wrapper. With exactly one wrapped
// instance it returns the inner result; with several it fails with
// ErrMultiInstance because this application runs one instance per core.
// Arrays that are ordinary payloads (peer lists, route lists) pass through.
func unwrapMulti(raw []byte) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return trimmed, nil
	}
	var items []instanceResult
	if err := json.Unmarshal(trimmed, &items); err != nil || len(items) == 0 {
		return trimmed, nil
	}
	for _, it := range items {
		if it.InstanceID == "" || len(it.Result) == 0 {
			return trimmed, nil
		}
	}
	if len(items) == 1 {
		return items[0].Result, nil
	}
	labels := make([]string, 0, len(items))
	for _, it := range items {
		label := it.InstanceName
		if label == "" {
			label = it.InstanceID
		}
		labels = append(labels, label)
	}
	return nil, multiInstanceError(nil, labels)
}

func decodeInto[T any](raw []byte, args []string) (*T, error) {
	payload, err := unwrapMulti(raw)
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil, parseError(args, fmt.Errorf("decode %T: %w", v, err))
	}
	return &v, nil
}

func decodeNode(raw []byte) (*NodeInfo, error) {
	return decodeInto[NodeInfo](raw, []string{"node"})
}

func decodePeers(raw []byte) ([]Peer, error) {
	v, err := decodeInto[[]Peer](raw, []string{"peer"})
	if err != nil {
		return nil, err
	}
	return *v, nil
}

func decodePeerVerbose(raw []byte) ([]PeerRoutePair, error) {
	v, err := decodeInto[[]PeerRoutePair](raw, []string{"-v", "peer"})
	if err != nil {
		return nil, err
	}
	return *v, nil
}

func decodeRoutes(raw []byte) ([]Route, error) {
	v, err := decodeInto[[]Route](raw, []string{"route"})
	if err != nil {
		return nil, err
	}
	return *v, nil
}

func decodeConnectors(raw []byte) ([]Connector, error) {
	v, err := decodeInto[[]Connector](raw, []string{"connector"})
	if err != nil {
		return nil, err
	}
	return *v, nil
}

// InterpretServiceStatus classifies the fixed English text of
// `service status` output. The CLI prints one sentence, not JSON:
// "Service is running", "Service is stopped" or "Service is not
// installed" (easytier-cli.rs). Unknown sentences leave installed
// unknown, which callers must not read as "not installed".
func InterpretServiceStatus(text string) (installed, running, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(lower, "not installed"):
		return false, false, true
	case strings.Contains(lower, "is running"):
		return true, true, true
	case strings.Contains(lower, "is stopped"):
		return true, false, true
	default:
		return false, false, false
	}
}
