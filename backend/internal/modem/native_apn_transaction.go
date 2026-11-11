package modem

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type NativeAPNProfile1Input struct{ profile savedAPNProfile }

func NewNativeAPNProfile1Input(apn, username, password string, authentication int) (NativeAPNProfile1Input, error) {
	if authentication < 0 || authentication > 3 {
		return NativeAPNProfile1Input{}, errors.New("APN_AUTH_UNSUPPORTED")
	}
	pdp, auth := byte(0), byte(authentication)
	profile := savedAPNProfile{index: 1, status: "available", pdp: &pdp, apn: &apn, username: &username, password: &password, authentication: &auth}
	if !profile.nativeFieldsComplete() {
		return NativeAPNProfile1Input{}, errors.New("APN_PROFILE1_INPUT_INVALID")
	}
	return NativeAPNProfile1Input{profile}, nil
}
func (NativeAPNProfile1Input) String() string     { return "[private native APN profile1]" }
func (v NativeAPNProfile1Input) GoString() string { return v.String() }
func (NativeAPNProfile1Input) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Private bool `json:"private"`
	}{true})
}

type NativeAPNApplyResult struct {
	ObservedAt              time.Time         `json:"observedAt"`
	Status                  string            `json:"status"`
	Source                  string            `json:"source"`
	BackupSaved             bool              `json:"backupSaved"`
	Attempted               bool              `json:"attempted"`
	Accepted                bool              `json:"accepted"`
	ConnectEventObserved    bool              `json:"connectEventObserved"`
	CompletionDelayMillis   *uint32           `json:"completionDelayMillis,omitempty"`
	Profile2Omitted         bool              `json:"profile2Omitted"`
	SavedFieldsMatched      *bool             `json:"savedFieldsMatched"`
	Profile2StillAbsent     *bool             `json:"profile2StillAbsent"`
	RouteRecoveryStatus     string            `json:"routeRecoveryStatus"`
	ManagementSettled       bool              `json:"managementSettled"`
	RouteRecoveryReason     string            `json:"routeRecoveryReason,omitempty"`
	ProfileReadbackReason   string            `json:"profileReadbackReason,omitempty"`
	InventoryReadbackReason string            `json:"inventoryReadbackReason,omitempty"`
	OwnerReadbackReason     string            `json:"ownerReadbackReason,omitempty"`
	Before                  *NativeAPNStatus  `json:"before"`
	After                   *NativeAPNStatus  `json:"after"`
	DataLink                NativeAPNDataLink `json:"dataLink"`
	Restored                bool              `json:"restored"`
	RecoveryRequired        bool              `json:"recoveryRequired"`
}

func validateNativeAPNRecoveryTime(observed, since, now time.Time) error {
	if observed.IsZero() || now.Sub(observed) > time.Hour || observed.After(now.Add(time.Minute)) {
		return errors.New("APN_NETWORK_BACKUP_STALE")
	}
	if !since.IsZero() && observed.Before(since) {
		return errors.New("APN_NETWORK_BACKUP_WRONG_JOB")
	}
	return nil
}

type nativeAPNTransactionIO struct {
	profiles     func(context.Context) (ProfileRecovery, error)
	inventory    func(context.Context) (ProfileInventory, error)
	status       func(context.Context) (NativeAPNStatus, error)
	connect      func(context.Context, savedAPNProfile) (NativeAPNStatus, error)
	network      func(context.Context) (nativeAPNNetworkBaseline, error)
	restoreRoute func(context.Context, nativeAPNNetworkBaseline) (string, error)
	settleRoute  func(context.Context, nativeAPNNetworkBaseline) (string, error)
	backup       func(ProfileRecovery, nativeAPNNetworkBaseline) error
	dataLink     func(context.Context) (NativeAPNDataLink, error)
}

var nativeAPNTransactionMu sync.Mutex

func defaultNativeAPNTransactionIO() nativeAPNTransactionIO {
	return nativeAPNTransactionIO{ReadGobiNetProfiles, ReadGobiNetProfileInventory, ReadNativeAPNStatus, nativeAPNConnectProfile1, captureNativeAPNNetwork, restoreNativeAPNDefault, settleNativeAPNDefault, saveNativeAPNTransactionBackup, nativeAPNDataLink}
}

// ApplyNativeAPNProfile1 accepts only typed, validated profile1 values. The
// owning root helper must serialize other native/network actions and pause our
// independent signal observer during fixed WDS read-backs. Saved credentials
// remain inside the root process and immutable private recovery files.
func ApplyNativeAPNProfile1(parent context.Context, input NativeAPNProfile1Input) (NativeAPNApplyResult, error) {
	return nativeAPNApply(parent, input, false)
}
func nativeAPNApply(parent context.Context, input NativeAPNProfile1Input, sameOnly bool) (NativeAPNApplyResult, error) {
	if e := requireProfileRecoveryRoot(); e != nil {
		return NativeAPNApplyResult{}, e
	}
	if !nativeAPNTransactionMu.TryLock() {
		return NativeAPNApplyResult{}, errors.New("APN_APPLY_IN_PROGRESS")
	}
	defer nativeAPNTransactionMu.Unlock()
	unlock, e := lockNativeAPNTransaction()
	if e != nil {
		return NativeAPNApplyResult{}, e
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	return runNativeAPNTransactionMode(ctx, input.profile, defaultNativeAPNTransactionIO(), sameOnly)
}
func nativeAPNVerifiedState(status NativeAPNStatus) bool {
	return status.Accepted && status.ResultReceived && status.ResultCode != nil && *status.ResultCode == 0 && status.Profile1State != nil && status.Profile2State != nil
}
func nativeAPNReadBack(ctx context.Context, want savedAPNProfile, dep nativeAPNTransactionIO, out *NativeAPNApplyResult) (savedAPNProfile, error) {
	var actual savedAPNProfile
	// Fixed GETs may race the owner's own short reconnect work. Retry GET only,
	// at most three times; never resend a timed-out connect request.
	for attempt := 0; attempt < 3; attempt++ {
		recovery, e := dep.profiles(ctx)
		if e == nil && recovery.profiles[0].nativeFieldsComplete() {
			actual = recovery.profiles[0]
			matched := sameNativeProfile1(want, actual)
			out.SavedFieldsMatched = &matched
			out.ProfileReadbackReason = ""
			if matched {
				break
			}
		}
		out.ProfileReadbackReason = "APN_SAVED_PROFILE_MISMATCH"
		if e != nil {
			out.ProfileReadbackReason = e.Error()
		} else if !recovery.profiles[0].nativeFieldsComplete() {
			out.ProfileReadbackReason = "APN_SAVED_PROFILE_UNAVAILABLE"
		}
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
	inventory, e := dep.inventory(ctx)
	if e == nil {
		absent := nativeAPNOnlyProfile1(inventory)
		out.Profile2StillAbsent = &absent
		out.InventoryReadbackReason = ""
	} else {
		out.InventoryReadbackReason = "APN_PROFILE_INVENTORY_UNAVAILABLE"
		if e != nil {
			out.InventoryReadbackReason = e.Error()
		}
	}
	status, e := dep.status(ctx)
	out.After = &status
	if e != nil || !nativeAPNVerifiedState(status) {
		out.OwnerReadbackReason = "APN_OWNER_STATUS_UNAVAILABLE"
		if e != nil {
			out.OwnerReadbackReason = e.Error()
		}
	} else {
		out.OwnerReadbackReason = ""
	}
	if out.SavedFieldsMatched == nil || out.Profile2StillAbsent == nil || out.OwnerReadbackReason != "" {
		return actual, errors.New("APN_READBACK_INCOMPLETE")
	}
	return actual, nil
}
func runNativeAPNTransaction(ctx context.Context, target savedAPNProfile, dep nativeAPNTransactionIO) (out NativeAPNApplyResult, err error) {
	return runNativeAPNTransactionMode(ctx, target, dep, false)
}
func runNativeAPNTransactionMode(ctx context.Context, target savedAPNProfile, dep nativeAPNTransactionIO, sameOnly bool) (out NativeAPNApplyResult, err error) {
	out = NativeAPNApplyResult{Status: "unknown", Source: "stock-npmcom-owner-profile1", Profile2Omitted: true, RouteRecoveryStatus: "not-attempted", DataLink: NativeAPNDataLink{Status: "unknown"}}
	if !sameOnly && (target.index != 1 || !target.nativeFieldsComplete()) {
		return out, errors.New("APN_PROFILE1_INPUT_INVALID")
	}
	network, e := dep.network(ctx)
	if e != nil {
		return out, e
	}
	original, e := dep.profiles(ctx)
	if e != nil || !original.profiles[0].nativeFieldsComplete() {
		return out, errors.New("APN_ORIGINAL_PROFILE_INCOMPLETE")
	}
	if sameOnly {
		target = original.profiles[0]
	}
	inventory, e := dep.inventory(ctx)
	if e != nil || !nativeAPNOnlyProfile1(inventory) {
		return out, errors.New("APN_PROFILE2_ABSENCE_NOT_VERIFIED")
	}
	before, e := dep.status(ctx)
	out.Before = &before
	if e != nil || !nativeAPNVerifiedState(before) {
		return out, errors.New("APN_OWNER_BASELINE_UNAVAILABLE")
	}
	if e = dep.backup(original, network); e != nil {
		return out, errors.New("APN_PRIVATE_BACKUP_FAILED")
	}
	out.BackupSaved = true
	// Even cancellation during the native call must not leave the proven WiFi
	// default missing. Cleanup has its own bound and adds only the exact route.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		previousRouteStatus := out.RouteRecoveryStatus
		out.RouteRecoveryStatus, e = dep.restoreRoute(cleanup, network)
		if previousRouteStatus == "restored" && out.RouteRecoveryStatus == "preserved" {
			out.RouteRecoveryStatus = "restored"
		}
		if e != nil {
			out.RouteRecoveryReason = e.Error()
			out.RecoveryRequired = true
			if err == nil {
				err = errors.New("APN_NETWORK_RECOVERY_UNVERIFIED")
				out.Status = "recovery-required"
			}
		} else {
			out.RouteRecoveryReason = ""
		}
		link, linkError := dep.dataLink(cleanup)
		out.DataLink = link
		if linkError != nil {
			out.DataLink.Status = "unknown"
		}
		out.ObservedAt = time.Now().UTC()
	}()
	out.Attempted = true
	completion, callError := dep.connect(ctx, target)
	out.Accepted = completion.Accepted && completion.ResultReceived && completion.ResultCode != nil && *completion.ResultCode == 0
	out.ConnectEventObserved = completion.Profile1State != nil
	out.CompletionDelayMillis = completion.CompletionDelayMillis
	// Restore the management path before potentially slower read-backs.
	cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	out.RouteRecoveryStatus, e = dep.restoreRoute(cleanup, network)
	cancel()
	if e != nil {
		out.RouteRecoveryReason = e.Error()
	}
	if dep.settleRoute != nil {
		settled, settleError := dep.settleRoute(ctx, network)
		if settled == "restored" || out.RouteRecoveryStatus != "restored" {
			out.RouteRecoveryStatus = settled
		}
		out.ManagementSettled = settleError == nil
		if settleError != nil {
			out.RouteRecoveryReason = settleError.Error()
		}
	}
	actual, readError := nativeAPNReadBack(ctx, target, dep, &out)
	if readError != nil {
		out.RecoveryRequired = true
		out.Status = "readback-incomplete"
		return out, readError
	}
	if !*out.Profile2StillAbsent {
		out.RecoveryRequired = true
		out.Status = "profile2-changed"
		return out, errors.New("APN_PROFILE2_CHANGED_NO_BLIND_RESTORE")
	}
	if !*out.SavedFieldsMatched {
		if sameNativeProfile1(original.profiles[0], actual) {
			out.Status = "unchanged"
			return out, errors.New("APN_PROFILE1_WAS_NOT_APPLIED")
		}
		out.RecoveryRequired = true
		out.Status = "conflict"
		return out, errors.New("APN_PROFILE_CONFLICT_NO_BLIND_OVERWRITE")
	}
	if callError != nil || !out.Accepted {
		out.Status = "readback-matched-acknowledgement-missing"
		return out, errors.New("APN_OWNER_COMPLETION_UNVERIFIED")
	}
	if !out.ManagementSettled {
		out.RecoveryRequired = true
		out.Status = "management-unsettled"
		return out, errors.New("APN_MANAGEMENT_PATH_UNSETTLED")
	}
	if *before.Profile1State == 3 && *out.After.Profile1State != 3 {
		// Revert only after the read-back proves the current saved fields are
		// exactly our attempted target. Do not overwrite a third party's change.
		latest, latestError := dep.profiles(ctx)
		if latestError != nil || !sameNativeProfile1(target, latest.profiles[0]) {
			out.RecoveryRequired = true
			out.Status = "conflict"
			return out, errors.New("APN_RESTORE_PRECONDITION_CHANGED")
		}
		restored, restoreError := dep.connect(ctx, original.profiles[0])
		_, verifyError := nativeAPNReadBack(ctx, original.profiles[0], dep, &out)
		out.Restored = restoreError == nil && restored.Accepted && verifyError == nil && out.SavedFieldsMatched != nil && *out.SavedFieldsMatched && out.Profile2StillAbsent != nil && *out.Profile2StillAbsent && out.After.Profile1State != nil && *out.After.Profile1State == 3
		out.RecoveryRequired = !out.Restored
		if out.Restored {
			out.Status = "restored"
			return out, errors.New("APN_PDP_FAILED_ORIGINAL_RESTORED")
		}
		out.Status = "recovery-required"
		return out, errors.New("APN_ORIGINAL_RESTORE_UNVERIFIED")
	}
	out.Status = "applied"
	return out, nil
}
