package nasbackup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	coreconfig "github.com/np1-control-center/ncc/internal/config"
)

type TargetChange struct {
	Protocol      *string `json:"protocol,omitempty"`
	Host          *string `json:"host,omitempty"`
	Port          *int    `json:"port,omitempty"`
	Share         *string `json:"share,omitempty"`
	Directory     *string `json:"directory,omitempty"`
	Scheme        *string `json:"scheme,omitempty"`
	HostKeySHA256 *string `json:"hostKeySHA256,omitempty"`
	Username      string  `json:"username,omitempty"`
	Password      string  `json:"password,omitempty"`
}

func (TargetChange) String() string     { return "NAS target change [redacted]" }
func (v TargetChange) GoString() string { return v.String() }

type PolicyChange struct {
	Enabled *bool                   `json:"enabled,omitempty"`
	Route   *string                 `json:"route,omitempty"`
	Timing  *string                 `json:"timing,omitempty"`
	SSID    *string                 `json:"ssid,omitempty"`
	Targets map[string]TargetChange `json:"targets,omitempty"`
}
type PoliciesChange struct {
	Policies map[string]PolicyChange `json:"policies"`
}

func (PoliciesChange) String() string     { return "NAS policy changes [redacted]" }
func (v PoliciesChange) GoString() string { return v.String() }

type TargetSettings struct {
	TargetView
	Configured         bool `json:"configured"`
	UsernameConfigured bool `json:"usernameConfigured"`
	PasswordConfigured bool `json:"passwordConfigured"`
}
type PolicyView struct {
	Enabled bool                      `json:"enabled"`
	Route   string                    `json:"route"`
	Timing  string                    `json:"timing"`
	SSID    string                    `json:"ssid"`
	Targets map[string]TargetSettings `json:"targets"`
}
type PoliciesView struct {
	Revision int                   `json:"revision"`
	Policies map[string]PolicyView `json:"policies"`
}
type storedInput ConfigInput
type storedPolicy struct {
	Enabled bool                   `json:"enabled"`
	Route   string                 `json:"route"`
	Timing  string                 `json:"timing"`
	SSID    string                 `json:"ssid"`
	Targets map[string]storedInput `json:"targets"`
}
type storedPolicies struct {
	Revision int                     `json:"revision"`
	Policies map[string]storedPolicy `json:"policies"`
}
type Policies struct{ data storedPolicies }

func (Policies) String() string                 { return "NAS policies [redacted]" }
func (p Policies) GoString() string             { return p.String() }
func (p Policies) MarshalJSON() ([]byte, error) { return json.Marshal(p.View()) }
func DefaultPolicies() Policies {
	p := Policies{storedPolicies{Policies: map[string]storedPolicy{}}}
	for _, kind := range []string{"video", "event"} {
		p.data.Policies[kind] = storedPolicy{Route: "wifi", Timing: "realtime", Targets: map[string]storedInput{"wifi": {Protocol: "smb"}, "tailscale": {Protocol: "smb"}}}
	}
	return p
}
func LegacyPolicies(c Config) Policies {
	p := DefaultPolicies()
	p.data.Revision = c.revision
	if c.host != "" {
		for _, kind := range []string{"video", "event"} {
			v := p.data.Policies[kind]
			v.Targets["wifi"] = storedInput{Protocol: c.protocol, Host: c.host, Port: c.port, Share: c.share, Directory: c.directory, Username: c.username, Password: c.password, Scheme: c.scheme, HostKeySHA256: c.hostKey}
			p.data.Policies[kind] = v
		}
	}
	return p
}
func (p Policies) Revision() int { return p.data.Revision }
func targetSettings(raw storedInput) TargetSettings {
	input := ConfigInput(raw)
	cfg, err := NewConfig(input)
	view := TargetView{Host: raw.Host, Share: raw.Share, Directory: raw.Directory, Protocol: raw.Protocol, Port: raw.Port, Scheme: raw.Scheme, HostKeySHA256: raw.HostKeySHA256}
	if err == nil {
		view = cfg.Target()
	}
	return TargetSettings{view, err == nil, raw.Username != "", raw.Password != ""}
}
func (p Policies) View() PoliciesView {
	out := PoliciesView{Revision: p.data.Revision, Policies: map[string]PolicyView{}}
	for kind, policy := range p.data.Policies {
		targets := map[string]TargetSettings{}
		for route, raw := range policy.Targets {
			targets[route] = targetSettings(raw)
		}
		out.Policies[kind] = PolicyView{policy.Enabled, policy.Route, policy.Timing, policy.SSID, targets}
	}
	return out
}
func (p Policies) Policy(kind string) (PolicyView, error) {
	view, ok := p.View().Policies[kind]
	if !ok {
		return PolicyView{}, errors.New("NAS_POLICY_KIND_INVALID")
	}
	return view, nil
}
func (p Policies) Target(kind, route string) (Config, error) {
	policy, ok := p.data.Policies[kind]
	if !ok {
		return Config{}, errors.New("NAS_POLICY_KIND_INVALID")
	}
	target, ok := policy.Targets[route]
	if !ok {
		return Config{}, errors.New("NAS_ROUTE_INVALID")
	}
	cfg, err := NewConfig(ConfigInput(target))
	if err != nil {
		return Config{}, errors.New("NAS_NOT_CONFIGURED")
	}
	return cfg, nil
}
func validateDraft(raw storedInput) error {
	input := ConfigInput(raw)
	if input.Protocol == "" {
		return errors.New("NAS_PROTOCOL_INVALID")
	}
	if input.Host == "" {
		input.Host = "nas.invalid"
	}
	if input.Share == "" {
		input.Share = "placeholder"
	}
	if input.Directory == "" {
		input.Directory = "placeholder"
	}
	if input.Username == "" {
		input.Username = "placeholder"
	}
	if input.Password == "" {
		input.Password = "placeholder"
	}
	if input.Protocol == "sftp" && input.HostKeySHA256 == "" {
		input.HostKeySHA256 = "SHA256:" + strings.Repeat("A", 43)
	}
	_, err := NewConfig(input)
	return err
}
func (p Policies) Changed(expected int, change PoliciesChange) (Policies, error) {
	if p.Revision() != expected {
		return Policies{}, errors.New("NAS_REVISION_CONFLICT")
	}
	if expected < 0 || expected == int(^uint(0)>>1) || len(change.Policies) == 0 || len(change.Policies) > 2 {
		return Policies{}, errors.New("NAS_CONFIG_INVALID")
	}
	// Roundtrip through an explicit private type gives independent maps without
	// invoking redacted public JSON or sharing mutable targets between policies.
	raw, _ := json.Marshal(p.data)
	var next storedPolicies
	_ = json.Unmarshal(raw, &next)
	out := Policies{next}
	out.data.Revision++
	for kind, update := range change.Policies {
		policy, ok := out.data.Policies[kind]
		if !ok {
			return Policies{}, errors.New("NAS_POLICY_KIND_INVALID")
		}
		if update.Enabled != nil {
			policy.Enabled = *update.Enabled
		}
		if update.Route != nil {
			policy.Route = *update.Route
		}
		if update.Timing != nil {
			policy.Timing = *update.Timing
		}
		if update.SSID != nil {
			policy.SSID = *update.SSID
		}
		if policy.Route != "wifi" && policy.Route != "tailscale" || policy.Timing != "realtime" && policy.Timing != "ssid" || len([]byte(policy.SSID)) > 32 || !utf8.ValidString(policy.SSID) || strings.ContainsRune(policy.SSID, 0) {
			return Policies{}, errors.New("NAS_POLICY_INVALID")
		}
		for route, patch := range update.Targets {
			target, ok := policy.Targets[route]
			if !ok {
				return Policies{}, errors.New("NAS_ROUTE_INVALID")
			}
			oldProtocol := target.Protocol
			if patch.Protocol != nil {
				target.Protocol = *patch.Protocol
			}
			if patch.Host != nil {
				target.Host = *patch.Host
			}
			if patch.Port != nil {
				target.Port = *patch.Port
			} else if target.Protocol != oldProtocol {
				target.Port = 0
			}
			if patch.Share != nil {
				target.Share = *patch.Share
			}
			if patch.Directory != nil {
				target.Directory = *patch.Directory
			}
			if patch.Scheme != nil {
				target.Scheme = *patch.Scheme
			}
			if patch.HostKeySHA256 != nil {
				target.HostKeySHA256 = *patch.HostKeySHA256
			}
			if patch.Username != "" {
				target.Username = patch.Username
			}
			if patch.Password != "" {
				target.Password = patch.Password
			}
			if err := validateDraft(target); err != nil {
				return Policies{}, err
			}
			policy.Targets[route] = target
		}
		if policy.Enabled {
			if policy.Timing == "ssid" && policy.SSID == "" {
				return Policies{}, errors.New("NAS_SSID_REQUIRED")
			}
			if _, err := NewConfig(ConfigInput(policy.Targets[policy.Route])); err != nil {
				return Policies{}, err
			}
		}
		out.data.Policies[kind] = policy
	}
	return out, nil
}
func LoadPolicies(filename string) (Policies, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return Policies{}, err
	}
	if privateRegular(info) != nil {
		return Policies{}, errors.New("NAS_POLICIES_UNSAFE")
	}
	file, err := os.Open(filename)
	if err != nil {
		return Policies{}, errors.New("NAS_POLICIES_UNAVAILABLE")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return Policies{}, errors.New("NAS_POLICIES_INVALID")
	}
	var data storedPolicies
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&data) != nil || decoder.Decode(new(any)) != io.EOF {
		return Policies{}, errors.New("NAS_POLICIES_INVALID")
	}
	p := Policies{data}
	if data.Revision < 0 || len(data.Policies) != 2 {
		return Policies{}, errors.New("NAS_POLICIES_INVALID")
	}
	for _, kind := range []string{"video", "event"} {
		policy, ok := data.Policies[kind]
		if !ok || len(policy.Targets) != 2 {
			return Policies{}, errors.New("NAS_POLICIES_INVALID")
		}
		if _, ok = policy.Targets["wifi"]; !ok {
			return Policies{}, errors.New("NAS_POLICIES_INVALID")
		}
		if _, ok = policy.Targets["tailscale"]; !ok {
			return Policies{}, errors.New("NAS_POLICIES_INVALID")
		}
	}
	// Apply each current policy through the same validator, without changing the
	// persisted revision; this also rejects unsupported routes and target data.
	change := PoliciesChange{Policies: map[string]PolicyChange{}}
	for kind, policy := range data.Policies {
		enabled, route, timing, ssid := policy.Enabled, policy.Route, policy.Timing, policy.SSID
		targets := map[string]TargetChange{}
		for route, raw := range policy.Targets {
			if err := validateDraft(raw); err != nil {
				return Policies{}, err
			}
			targets[route] = TargetChange{}
		}
		change.Policies[kind] = PolicyChange{&enabled, &route, &timing, &ssid, targets}
	}
	if _, err = p.Changed(data.Revision, change); err != nil {
		return Policies{}, err
	}
	return p, nil
}
func SavePolicies(filename string, p Policies) error {
	if err := prepareDirectory(filepath.Dir(filename)); err != nil {
		return err
	}
	return coreconfig.Atomic(filename, p.data)
}
func UpdatePolicies(filename string, expected int, change PoliciesChange, legacy Config) (Policies, error) {
	if err := prepareDirectory(filepath.Dir(filename)); err != nil {
		return Policies{}, err
	}
	unlock, err := lockState(filepath.Dir(filename))
	if err != nil {
		return Policies{}, err
	}
	defer unlock()
	previous, err := LoadPolicies(filename)
	if os.IsNotExist(err) {
		previous = LegacyPolicies(legacy)
	} else if err != nil {
		return Policies{}, err
	}
	next, err := previous.Changed(expected, change)
	if err != nil {
		return Policies{}, err
	}
	if err = SavePolicies(filename, next); err != nil {
		return Policies{}, err
	}
	return next, nil
}
