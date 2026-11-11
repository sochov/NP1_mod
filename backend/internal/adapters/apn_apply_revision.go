package adapters

import (
	"errors"
	"github.com/np1-control-center/ncc/internal/modem"
	"reflect"
	"time"
)

// The callback remains in this root process. Credentials never need to cross
// the worker socket, argv, environment, or a public JSON representation.
func WithAPNApplyRevision(expected int, apply func(modem.NativeAPNProfile1Input) error) (APNSettingsView, error) {
	return apnSettings.WithApplyRevision(expected, apply)
}
func (s *APNStore) WithApplyRevision(expected int, apply func(modem.NativeAPNProfile1Input) error) (APNSettingsView, error) {
	unlock, e := s.mutationLock()
	if e != nil {
		return APNSettingsView{}, e
	}
	defer unlock()
	before, e := s.load()
	if e != nil {
		return APNSettingsView{}, e
	}
	if expected != before.Revision {
		return apnPublic(before), errors.New("REVISION_CONFLICT")
	}
	if apply == nil {
		return apnPublic(before), errors.New("APN_APPLY_UNAVAILABLE")
	}
	if !before.Profiles[0].Enabled || before.Profiles[1].Enabled {
		return apnPublic(before), errors.New("APN_PROFILE1_ONLY_SUPPORTED")
	}
	profile := before.Profiles[0]
	input, e := modem.NewNativeAPNProfile1Input(profile.APN, profile.Username, profile.Password, profile.Auth)
	if e != nil {
		return apnPublic(before), e
	}
	if e = apply(input); e != nil {
		return apnPublic(before), e
	}
	after, e := s.load()
	if e != nil {
		return APNSettingsView{}, e
	}
	if after.Revision != expected || !reflect.DeepEqual(after.Profiles, before.Profiles) {
		return apnPublic(after), errors.New("REVISION_CONFLICT")
	}
	at := time.Now().UTC()
	after.AppliedRevision = &expected
	after.AppliedAt = &at
	if e = s.write(after); e != nil {
		return APNSettingsView{}, e
	}
	return apnPublic(after), nil
}
