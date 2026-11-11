package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/adapters"
	"github.com/np1-control-center/ncc/internal/config"
	"github.com/np1-control-center/ncc/internal/modem"
)

type apnApplyJob struct {
	ID              string                      `json:"id"`
	Revision        int                         `json:"revision"`
	Status          string                      `json:"status"`
	StartedAt       time.Time                   `json:"startedAt"`
	FinishedAt      *time.Time                  `json:"finishedAt,omitempty"`
	Code            string                      `json:"code,omitempty"`
	Verified        bool                        `json:"verified"`
	AppliedRevision *int                        `json:"appliedRevision,omitempty"`
	Result          *modem.NativeAPNApplyResult `json:"result,omitempty"`
}
type apnOperation func(context.Context, int) (adapters.APNSettingsView, modem.NativeAPNApplyResult, error)
type apnJobs struct {
	mu        sync.Mutex
	job       *apnApplyJob
	operation apnOperation
	save      func(apnApplyJob) error
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

var apnJobID = regexp.MustCompile(`^[a-f0-9]{48}$`)
var apnErrorCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,80}$`)

func newAPNJobs(operation apnOperation, save func(apnApplyJob) error) *apnJobs {
	ctx, cancel := context.WithCancel(context.Background())
	return &apnJobs{operation: operation, save: save, ctx: ctx, cancel: cancel}
}
func (j *apnJobs) close() { j.cancel(); j.wg.Wait() }
func (j *apnJobs) start(revision int) (apnApplyJob, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.ctx.Err() != nil {
		return apnApplyJob{}, errors.New("APN_WORKER_STOPPING")
	}
	if j.job != nil && (j.job.Status == "queued" || j.job.Status == "running") {
		return apnApplyJob{}, errors.New("APN_APPLY_IN_PROGRESS")
	}
	if revision < 1 {
		return apnApplyJob{}, errors.New("INVALID_APN_REVISION")
	}
	var nonce [24]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return apnApplyJob{}, e
	}
	job := apnApplyJob{ID: hex.EncodeToString(nonce[:]), Revision: revision, Status: "queued", StartedAt: time.Now().UTC()}
	if e := j.save(job); e != nil {
		return apnApplyJob{}, errors.New("APN_JOB_SAVE_FAILED")
	}
	j.job = &job
	j.wg.Add(1)
	go j.run(job.ID, revision)
	return job, nil
}
func (j *apnJobs) run(id string, revision int) {
	defer j.wg.Done()
	j.mu.Lock()
	j.job.Status = "running"
	if e := j.save(*j.job); e != nil {
		j.job.Status = "failed"
		j.job.Code = "APN_JOB_SAVE_FAILED"
		j.mu.Unlock()
		return
	}
	j.mu.Unlock()
	view, result, e := j.operation(j.ctx, revision)
	now := time.Now().UTC()
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.job == nil || j.job.ID != id {
		return
	}
	j.job.FinishedAt = &now
	j.job.Result = &result
	j.job.Status = "succeeded"
	j.job.Verified = e == nil
	if e == nil {
		j.job.AppliedRevision = view.AppliedRevision
	} else {
		j.job.Status = "failed"
		j.job.Code = "APN_APPLY_FAILED"
		if apnErrorCode.MatchString(e.Error()) {
			j.job.Code = e.Error()
		}
	}
	if e := j.save(*j.job); e != nil {
		j.job.Code = "APN_JOB_SAVE_FAILED"
		j.job.Status = "failed"
		j.job.Verified = false
	}
}
func (j *apnJobs) get(id string) (apnApplyJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.job == nil || j.job.ID != id {
		return apnApplyJob{}, false
	}
	return *j.job, true
}

const observerPausedPath = "/run/np1-control-apn/observer-paused.json"

func apnService(parent context.Context, action string) error {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", action, "np1-control-modem.service").Run()
}
func runAPNOperation(ctx context.Context, revision int) (view adapters.APNSettingsView, result modem.NativeAPNApplyResult, err error) {
	view, err = adapters.WithAPNApplyRevision(revision, func(input modem.NativeAPNProfile1Input) (operationError error) {
		active := exec.Command("systemctl", "is-active", "--quiet", "np1-control-modem.service").Run() == nil
		if active {
			if e := config.Atomic(observerPausedPath, map[string]bool{"resume": true}); e != nil {
				return errors.New("APN_OBSERVER_MARKER_FAILED")
			}
			defer func() {
				if apnService(context.Background(), "start") != nil {
					operationError = errors.New("APN_OBSERVER_RESUME_FAILED")
				} else {
					_ = os.Remove(observerPausedPath)
				}
			}()
			if apnService(ctx, "stop") != nil {
				return errors.New("APN_OBSERVER_PAUSE_FAILED")
			}
		}
		var e error
		result, e = modem.ApplyNativeAPNProfile1(ctx, input)
		return e
	})
	return
}

// ServeAPN isolates the modem's native write authority from the Web process.
// IPC contains only a revision; private APN credentials stay inside this worker.
func ServeAPN(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return errors.New("APN worker requires root")
	}
	const socket = adapters.APNControlSocket
	if info, e := os.Lstat(socket); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("APN socket path occupied")
		}
		if e = os.Remove(socket); e != nil {
			return e
		}
	}
	l, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer l.Close()
	if e = os.Chmod(socket, 0600); e != nil {
		return e
	}
	const state = "/run/np1-control-apn/job.json"
	jobs := newAPNJobs(runAPNOperation, func(job apnApplyJob) error { return config.Atomic(state, job) })
	defer jobs.close()
	if info, e := os.Lstat(observerPausedPath); e == nil && info.Mode().IsRegular() {
		if apnService(context.Background(), "start") == nil {
			_ = os.Remove(observerPausedPath)
		}
	}
	// A worker restart does not resend a native command. Recover only an exact
	// missing management route using the transaction's validated private proof.
	if raw, e := os.ReadFile(state); e == nil {
		var previous apnApplyJob
		if json.Unmarshal(raw, &previous) == nil && apnJobID.MatchString(previous.ID) {
			if previous.Status == "queued" || previous.Status == "running" {
				recovery, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				_, recoverError := modem.RecoverNativeAPNManagementRoute(recovery)
				cancel()
				previous.Status = "failed"
				previous.Code = "APN_WORKER_INTERRUPTED"
				previous.Verified = false
				if recoverError != nil {
					previous.Code = "APN_INTERRUPTED_RECOVERY_UNVERIFIED"
				}
				now := time.Now().UTC()
				previous.FinishedAt = &now
				_ = jobs.save(previous)
			}
			jobs.job = &previous
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /apply", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int `json:"expectedRevision"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
		d.DisallowUnknownFields()
		var extra any
		if d.Decode(&body) != nil || d.Decode(&extra) != io.EOF {
			apnWorkerReply(w, 400, map[string]string{"code": "INVALID_APN_REVISION"})
			return
		}
		job, e := jobs.start(body.ExpectedRevision)
		if e != nil {
			status := 503
			if e.Error() == "APN_APPLY_IN_PROGRESS" {
				status = 409
			}
			if e.Error() == "INVALID_APN_REVISION" {
				status = 400
			}
			apnWorkerReply(w, status, map[string]string{"code": e.Error()})
			return
		}
		apnWorkerReply(w, 202, job)
	})
	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !apnJobID.MatchString(id) {
			apnWorkerReply(w, 400, map[string]string{"code": "INVALID_APN_JOB"})
			return
		}
		job, ok := jobs.get(id)
		if !ok {
			apnWorkerReply(w, 404, map[string]string{"code": "APN_JOB_NOT_FOUND"})
			return
		}
		apnWorkerReply(w, 200, job)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096}
	go func() {
		<-ctx.Done()
		jobs.cancel()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	e = server.Serve(listener{l, 0})
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
func apnWorkerReply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
