package modem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func nativeAPNFixtureStatus(active bool) NativeAPNStatus {
	code, a, b := uint32(0), uint32(3), uint32(2)
	if !active {
		a = 2
	}
	return NativeAPNStatus{Accepted: true, ResultReceived: true, ResultCode: &code, Profile1State: &a, Profile2State: &b}
}
func nativeAPNFixtureIO(t *testing.T) (nativeAPNTransactionIO, *savedAPNProfile, *int) {
	t.Helper()
	original := syntheticRecovery(t)
	current := original.profiles[0]
	calls := 0
	dep := nativeAPNTransactionIO{
		profiles: func(context.Context) (ProfileRecovery, error) {
			out := original
			out.profiles[0] = current
			return out, nil
		},
		inventory: func(context.Context) (ProfileInventory, error) {
			return ProfileInventory{Indices: []int{1}, Profile1Present: true}, nil
		},
		status: func(context.Context) (NativeAPNStatus, error) { return nativeAPNFixtureStatus(true), nil },
		connect: func(_ context.Context, p savedAPNProfile) (NativeAPNStatus, error) {
			calls++
			current = p
			out := nativeAPNFixtureStatus(true)
			a, b := uint32(2), uint32(1)
			out.Profile1State = &a
			out.Profile2State = &b
			return out, nil
		},
		network:      func(context.Context) (nativeAPNNetworkBaseline, error) { return nativeAPNNetworkBaseline{}, nil },
		restoreRoute: func(context.Context, nativeAPNNetworkBaseline) (string, error) { return "restored", nil },
		settleRoute:  func(context.Context, nativeAPNNetworkBaseline) (string, error) { return "restored", nil },
		backup:       func(ProfileRecovery, nativeAPNNetworkBaseline) error { return nil },
		dataLink: func(context.Context) (NativeAPNDataLink, error) {
			no := false
			return NativeAPNDataLink{Status: "available", ProcessRunning: &no, LinuxEgressConfigured: &no}, nil
		},
	}
	return dep, &current, &calls
}
func nativeAPNTarget(t *testing.T) savedAPNProfile {
	t.Helper()
	input, e := NewNativeAPNProfile1Input("new.example", "fixture-user", "fixture-password", 1)
	if e != nil {
		t.Fatal(e)
	}
	return input.profile
}
func TestNativeAPNTransactionReadbackAndManagementRecovery(t *testing.T) {
	dep, _, calls := nativeAPNFixtureIO(t)
	out, e := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if e != nil || out.Status != "applied" || *calls != 1 || !out.Accepted || out.SavedFieldsMatched == nil || !*out.SavedFieldsMatched || out.Profile2StillAbsent == nil || !*out.Profile2StillAbsent || out.RouteRecoveryStatus != "restored" {
		t.Fatal(out, e, *calls)
	}
	if out.DataLink.LinuxEgressConfigured == nil || *out.DataLink.LinuxEgressConfigured {
		t.Fatal("native active status fabricated Linux egress")
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "fixture-password") || strings.Contains(string(raw), "new.example") {
		t.Fatal("private APN leaked")
	}
}
func TestNativeAPNTransactionRefusesUnprovenProfile2(t *testing.T) {
	dep, _, calls := nativeAPNFixtureIO(t)
	dep.inventory = func(context.Context) (ProfileInventory, error) {
		return ProfileInventory{Indices: []int{1, 2}, Profile1Present: true, Profile2Present: true}, nil
	}
	out, e := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if e == nil || *calls != 0 || out.Attempted {
		t.Fatal("profile2 conflict mutated", out, e)
	}
}
func TestNativeAPNTransactionMissingReadbackIsUnknownAndNoRetry(t *testing.T) {
	dep, _, calls := nativeAPNFixtureIO(t)
	originalRead := dep.profiles
	dep.profiles = func(ctx context.Context) (ProfileRecovery, error) {
		if *calls > 0 {
			return ProfileRecovery{}, errors.New("QMI channel already opened by another process")
		}
		return originalRead(ctx)
	}
	out, e := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if e == nil || out.SavedFieldsMatched != nil || !out.RecoveryRequired || *calls != 1 || out.ProfileReadbackReason == "" {
		t.Fatal(out, e, *calls)
	}
}
func TestNativeAPNTransactionDoesNotOverwriteConcurrentSettings(t *testing.T) {
	dep, current, calls := nativeAPNFixtureIO(t)
	originalConnect := dep.connect
	dep.connect = func(ctx context.Context, p savedAPNProfile) (NativeAPNStatus, error) {
		out, e := originalConnect(ctx, p)
		third := *current
		changed := "third.example"
		third.apn = &changed
		*current = third
		return out, e
	}
	out, e := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if e == nil || out.Status != "conflict" || *calls != 1 || !out.RecoveryRequired {
		t.Fatal(out, e, *calls)
	}
}
func TestNativeAPNTransactionRestoresOriginalOnlyAfterOwnTargetProof(t *testing.T) {
	dep, _, calls := nativeAPNFixtureIO(t)
	dep.status = func(context.Context) (NativeAPNStatus, error) { return nativeAPNFixtureStatus(*calls != 1), nil }
	out, e := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if e == nil || out.Status != "restored" || !out.Restored || out.RecoveryRequired || *calls != 2 {
		t.Fatal(out, e, *calls)
	}
}
func TestNativeAPNTransactionBlockedRouteCannotReportSuccess(t *testing.T) {
	dep, _, _ := nativeAPNFixtureIO(t)
	dep.restoreRoute = func(context.Context, nativeAPNNetworkBaseline) (string, error) {
		return "blocked", errors.New("APN_COMPETING_DEFAULT_ROUTE")
	}
	out, e := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if e == nil || out.Status != "recovery-required" || !out.RecoveryRequired {
		t.Fatal(out, e)
	}
}
func TestNativeAPNProfileInputRedactsEveryFormat(t *testing.T) {
	input, e := NewNativeAPNProfile1Input("private.example", "secret-user", "secret-password", 3)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(input)
	for _, output := range []string{string(raw), fmt.Sprint(input), fmt.Sprintf("%#v", input), fmt.Sprintf("%+v", input)} {
		for _, secret := range []string{"private.example", "secret-user", "secret-password"} {
			if strings.Contains(output, secret) {
				t.Fatal("secret leaked")
			}
		}
	}
	for _, value := range []string{"quote\"", "semi;colon", "space value", "back\\slash", "-option"} {
		if _, e := NewNativeAPNProfile1Input("valid.example", value, "", 0); e == nil {
			t.Fatal("unsafe credential accepted")
		}
	}
}
func TestNativeAPNTransactionCannotSucceedBeforeManagementSettles(t *testing.T) {
	dep, _, _ := nativeAPNFixtureIO(t)
	dep.settleRoute = func(context.Context, nativeAPNNetworkBaseline) (string, error) {
		return "blocked", errors.New("APN_MANAGEMENT_SETTLE_INTERRUPTED")
	}
	out, e := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if e == nil || out.ManagementSettled || out.Status != "management-unsettled" || !out.RecoveryRequired {
		t.Fatal(out, e)
	}
}
func TestNativeAPNTransactionBackupFailureDoesNotWrite(t *testing.T) {
	dep, _, calls := nativeAPNFixtureIO(t)
	dep.backup = func(ProfileRecovery, nativeAPNNetworkBaseline) error { return errors.New("fixture disk full") }
	out, e := runNativeAPNTransaction(context.Background(), nativeAPNTarget(t), dep)
	if e == nil || out.Attempted || *calls != 0 {
		t.Fatal(out, e, *calls)
	}
}
