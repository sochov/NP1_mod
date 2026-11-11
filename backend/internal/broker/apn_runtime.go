package broker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

func readAPNRuntimeJSON(path string, target any, limit int64) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit || !trustedAPNRuntime(info) {
		return errors.New("APN_RUNTIME_STATE_INVALID")
	}
	f, e := os.OpenFile(path, os.O_RDONLY|apnRuntimeNoFollow(), 0)
	if e != nil {
		return errors.New("APN_RUNTIME_STATE_INVALID")
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(opened, info) {
		return errors.New("APN_RUNTIME_STATE_INVALID")
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return errors.New("APN_RUNTIME_STATE_INVALID")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("APN_RUNTIME_STATE_INVALID")
	}
	return nil
}

func readAPNPersistedJob(path string, now time.Time) (apnApplyJob, error) {
	var job apnApplyJob
	if e := readAPNRuntimeJSON(path, &job, 32768); e != nil {
		return job, e
	}
	if !apnJobID.MatchString(job.ID) || job.Revision < 1 || job.StartedAt.IsZero() || job.StartedAt.After(now.Add(time.Minute)) {
		return apnApplyJob{}, errors.New("APN_JOB_STATE_INVALID")
	}
	if job.Status != "queued" && job.Status != "running" && job.Status != "failed" && job.Status != "succeeded" || job.Code != "" && !apnErrorCode.MatchString(job.Code) {
		return apnApplyJob{}, errors.New("APN_JOB_STATE_INVALID")
	}
	if job.Status == "succeeded" && (!job.Verified || job.Result == nil || job.AppliedRevision == nil || *job.AppliedRevision != job.Revision || !verifiedAPNConfiguration(*job.Result)) {
		return apnApplyJob{}, errors.New("APN_JOB_STATE_INVALID")
	}
	return job, nil
}
