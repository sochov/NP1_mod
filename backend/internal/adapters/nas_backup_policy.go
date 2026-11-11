package adapters

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/np1-control-center/ncc/internal/model"
	"github.com/np1-control-center/ncc/internal/nasbackup"
)

func (c *nasBackupController) savePolicies(expected int, change nasbackup.PoliciesChange) (model.Observation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy() {
		return model.Observation{}, errors.New("NAS_BUSY")
	}
	legacy, err := c.config()
	if err != nil {
		return model.Observation{}, err
	}
	if _, err = nasbackup.UpdatePolicies(filepath.Join(c.directory, "policies.json"), expected, change, legacy); err != nil {
		return model.Observation{}, err
	}
	for kind := range change.Policies {
		c.paused[kind] = false
		c.nextTry[kind] = time.Time{}
	}
	c.saveAutomatic()
	c.connection = NASConnectionState{Status: "unknown"}
	c.connections = map[string]map[string]NASConnectionState{"video": {}, "event": {}}
	data, err := c.data()
	if err != nil {
		return model.Observation{}, err
	}
	return model.Value("NAS backup private controller", data), nil
}
func (c *nasBackupController) testPolicy(ctx context.Context, expected int, kind, route string) (model.Observation, error) {
	c.mu.Lock()
	if c.busy() {
		c.mu.Unlock()
		return model.Observation{}, errors.New("NAS_BUSY")
	}
	policies, err := c.policies()
	if err != nil {
		c.mu.Unlock()
		return model.Observation{}, err
	}
	if policies.Revision() != expected {
		c.mu.Unlock()
		return model.Observation{}, errors.New("NAS_REVISION_CONFLICT")
	}
	policy, err := policies.Policy(kind)
	if err != nil {
		c.mu.Unlock()
		return model.Observation{}, err
	}
	if route == "" {
		route = policy.Route
	}
	cfg, err := policies.Target(kind, route)
	if err != nil {
		c.mu.Unlock()
		return model.Observation{}, err
	}
	release, err := nasbackup.ClaimController(c.directory)
	if err != nil {
		c.mu.Unlock()
		return model.Observation{}, err
	}
	defer release()
	current, err := c.policies()
	if err != nil || current.Revision() != expected {
		c.mu.Unlock()
		return model.Observation{}, errors.New("NAS_REVISION_CONFLICT")
	}
	c.testing = true
	c.connection = NASConnectionState{Status: "testing", ObservedAt: time.Now().UTC()}
	if c.connections[kind] == nil {
		c.connections[kind] = map[string]NASConnectionState{}
	}
	c.connections[kind][route] = c.connection
	c.mu.Unlock()
	binding, probeErr := c.prepare(ctx, nasRouteRule{route: route, timing: policy.Timing, ssid: policy.SSID}, cfg.Target())
	if probeErr == nil {
		var remote nasRemote
		remote, probeErr = c.connect(ctx, cfg, binding.transport())
		if probeErr == nil {
			probeErr = binding.check(ctx)
			_ = remote.Close()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.testing = false
	state := NASConnectionState{Status: "failed", ObservedAt: time.Now().UTC()}
	if probeErr == nil && ctx.Err() == nil {
		state.Status = "available"
	} else {
		_, state.Code, state.Reason = NASBackupErrorStatus(probeErr)
	}
	c.connection = state
	c.connections[kind][route] = state
	data, err := c.data()
	if err != nil {
		return model.Observation{}, err
	}
	if state.Status != "available" {
		return model.Value("NAS backup private controller", data), errors.New(state.Code)
	}
	return model.Value("NAS backup private controller", data), nil
}
func SaveNASPolicies(expected int, change nasbackup.PoliciesChange) (model.Observation, error) {
	return nasBackups.savePolicies(expected, change)
}
func TestNASBackupPolicy(ctx context.Context, expected int, kind, route string) (model.Observation, error) {
	return nasBackups.testPolicy(ctx, expected, kind, route)
}
