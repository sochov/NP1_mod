package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/modem"
)

const apnWorkerExecutable = "/run/np1-control-apn/ncc"
const apnHelperMode = "apn-apply-revision"
const apnHelperTimeout = 90 * time.Second
const apnHelperOutputLimit = 24 * 1024

// The child owns the editor lock for the complete transaction. Only a revision
// crosses argv; neither public APN text nor credentials cross its output pipe.
type apnChildResult struct {
	Version         int                        `json:"version"`
	Revision        int                        `json:"revision"`
	AppliedRevision *int                       `json:"appliedRevision,omitempty"`
	Result          modem.NativeAPNApplyResult `json:"result"`
	Code            string                     `json:"code,omitempty"`
}

type apnOutput struct{ bytes.Buffer }

func (b *apnOutput) Write(p []byte) (int, error) {
	if len(p) > apnHelperOutputLimit-b.Len() {
		return 0, errors.New("APN_HELPER_OUTPUT_OVERSIZE")
	}
	return b.Buffer.Write(p)
}

func apnSafeCode(e error) string {
	if e != nil && apnErrorCode.MatchString(e.Error()) {
		return e.Error()
	}
	return "APN_APPLY_FAILED"
}

func fixedAPNExecutable() (string, error) {
	name, e := os.Executable()
	if e == nil {
		name, e = filepath.EvalSymlinks(name)
	}
	if e != nil || name != apnWorkerExecutable || os.Geteuid() != 0 {
		return "", errors.New("APN_FIXED_WORKER_REQUIRED")
	}
	return name, nil
}

// RunAPNApplyRevision is the fixed child entry point, never an HTTP operation.
// Errors are represented by fixed codes; private vendor diagnostics are not
// copied into stderr, the job file, or the socket response.
func RunAPNApplyRevision(ctx context.Context, revision int, output io.Writer) error {
	response := apnChildResult{Version: 1, Revision: revision}
	_, e := fixedAPNExecutable()
	if e == nil && !adapters.APNFieldReady() {
		e = errors.New("APN_WORKER_FIELD_VALIDATION_REQUIRED")
	}
	if e == nil && revision < 1 {
		e = errors.New("INVALID_APN_REVISION")
	}
	if e == nil {
		var view adapters.APNSettingsView
		view, response.Result, e = runAPNChildOperation(ctx, revision)
		if e == nil && !verifiedAPNResult(view, response.Result, revision) {
			e = errors.New("APN_RESULT_UNVERIFIED")
		}
		if e == nil {
			response.AppliedRevision = view.AppliedRevision
		}
	}
	if e != nil {
		response.Code = apnSafeCode(e)
	}
	return json.NewEncoder(output).Encode(response)
}

func runAPNChildOperation(ctx context.Context, revision int) (view adapters.APNSettingsView, result modem.NativeAPNApplyResult, err error) {
	view, err = adapters.WithAPNApplyRevision(revision, func(input modem.NativeAPNProfile1Input) error {
		return withAPNObserverPaused(ctx, realAPNObserver(), func(ctx context.Context) error {
			// Route cleanup and observer recovery have independent bounds after
			// cancellation. The parent additionally kills and reaps this process.
			apply, cancel := context.WithTimeout(ctx, 35*time.Second)
			defer cancel()
			var e error
			result, e = modem.ApplyNativeAPNProfile1(apply, input)
			if e == nil && !verifiedAPNConfiguration(result) {
				e = errors.New("APN_RESULT_UNVERIFIED")
			}
			return e
		})
	})
	return
}

func parseAPNChildResult(raw []byte, revision int) (apnChildResult, error) {
	var result apnChildResult
	if len(raw) > apnHelperOutputLimit {
		return result, errors.New("APN_HELPER_OUTPUT_OVERSIZE")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || result.Version != 1 || result.Revision != revision || result.Code != "" && !apnErrorCode.MatchString(result.Code) {
		return apnChildResult{}, errors.New("APN_HELPER_RESULT_INVALID")
	}
	if result.Code != "" {
		if result.AppliedRevision != nil {
			return apnChildResult{}, errors.New("APN_HELPER_RESULT_INVALID")
		}
		return result, errors.New(result.Code)
	}
	if !verifiedAPNResult(adapters.APNSettingsView{AppliedRevision: result.AppliedRevision}, result.Result, revision) {
		return result, errors.New("APN_RESULT_UNVERIFIED")
	}
	return result, nil
}

func runAPNOperation(ctx context.Context, revision int) (adapters.APNSettingsView, modem.NativeAPNApplyResult, error) {
	path, e := fixedAPNExecutable()
	if e != nil {
		return adapters.APNSettingsView{}, modem.NativeAPNApplyResult{}, e
	}
	started := time.Now().UTC()
	output, canceled, e := executeAPNProcess(ctx, apnHelperTimeout, func(bounded context.Context) *exec.Cmd {
		cmd := exec.CommandContext(bounded, path, "--mode", apnHelperMode, "--apn-revision", apnRevisionText(revision))
		cmd.Env = []string{"PATH=/sbin:/bin:/usr/sbin:/usr/bin", "LC_ALL=C"}
		return cmd
	})
	if e != nil {
		code := "APN_HELPER_FAILED"
		if canceled {
			code = "APN_HELPER_TIMEOUT"
			if ctx.Err() != nil {
				code = "APN_WORKER_STOPPING"
			}
		}
		if recoverAPNChildFailure(started) != nil {
			code = "APN_HELPER_RECOVERY_UNVERIFIED"
		}
		return adapters.APNSettingsView{}, modem.NativeAPNApplyResult{}, errors.New(code)
	}
	response, e := parseAPNChildResult(output, revision)
	if e != nil && (response.Version == 0 || response.Code == "") {
		if recoverAPNChildFailure(started) != nil {
			e = errors.New("APN_HELPER_RECOVERY_UNVERIFIED")
		}
	}
	if e != nil && response.Code != "" {
		if resumeMarkedAPNObserver(context.Background(), realAPNObserver()) != nil {
			e = errors.New("APN_OBSERVER_RESUME_UNVERIFIED")
		}
	}
	return adapters.APNSettingsView{AppliedRevision: response.AppliedRevision}, response.Result, e
}

func apnWorkerReady() bool {
	if !adapters.APNFieldReady() {
		return false
	}
	_, e := os.Lstat(observerPausedPath)
	return errors.Is(e, os.ErrNotExist)
}

func executeAPNProcess(ctx context.Context, timeout time.Duration, command func(context.Context) *exec.Cmd) ([]byte, bool, error) {
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(bounded)
	cmd.WaitDelay = time.Second
	var output apnOutput
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	e := cmd.Run() // No next operation starts before this Wait/reap completes.
	return output.Bytes(), bounded.Err() != nil, e
}

func recoverAPNChildFailure(started time.Time) error {
	cleanup, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	_, routeError := modem.RecoverNativeAPNManagementRouteSince(cleanup, started)
	cancel()
	resumeError := resumeMarkedAPNObserver(context.Background(), realAPNObserver())
	if resumeError != nil {
		return resumeError
	}
	// A crash before a new transaction snapshot must not use an older snapshot.
	if routeError != nil && routeError.Error() != "APN_NETWORK_BACKUP_WRONG_JOB" && !errors.Is(routeError, os.ErrNotExist) {
		return errors.New("APN_NETWORK_RECOVERY_UNVERIFIED")
	}
	return nil
}

type apnPauseMarker struct {
	Version   int       `json:"version"`
	Resume    bool      `json:"resume"`
	StartedAt time.Time `json:"startedAt"`
}

func apnRevisionText(revision int) string {
	// Only an integer reaches argv. There is no generic command/input argument.
	return strconv.Itoa(revision)
}

func saveAPNPauseMarker(started time.Time) error {
	return config.Atomic(observerPausedPath, apnPauseMarker{1, true, started})
}
