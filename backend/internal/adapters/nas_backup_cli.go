package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/np1-control-center/ncc/internal/nasbackup"
)

func nasCLIDecode(input io.Reader, value any) error {
	decoder := json.NewDecoder(io.LimitReader(input, 16385))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("NAS_CLI_INPUT_INVALID")
	}
	return nil
}

// RunNASBackupCLI accepts only fixed modes. Credentials/recording IDs arrive
// via stdin; paths, URLs and secrets are never command-line arguments.
func RunNASBackupCLI(ctx context.Context, mode string, input io.Reader, output io.Writer) error {
	encoder := json.NewEncoder(output)
	switch mode {
	case "nas-config-seed":
		var request nasbackup.ConfigInput
		if err := nasCLIDecode(input, &request); err != nil {
			return err
		}
		nasBackups.mu.Lock()
		cfg, err := nasBackups.config()
		configured := cfg.View().Configured
		nasBackups.mu.Unlock()
		if err != nil {
			return err
		}
		if configured {
			return errors.New("NAS_CONFIG_EXISTS")
		}
		if _, err = SaveNASBackup(0, request); err != nil {
			return err
		}
		return encoder.Encode(map[string]any{"status": "saved", "revision": 1, "credentialsPrinted": false})
	case "nas-probe":
		nasBackups.mu.Lock()
		cfg, err := nasBackups.config()
		nasBackups.mu.Unlock()
		if err != nil {
			return err
		}
		_, err = TestNASBackup(ctx, cfg.Revision())
		result := map[string]any{"status": "unavailable", "protocol": "SMB2/3", "directoryReadable": false, "writing": false}
		if err == nil {
			result["status"], result["directoryReadable"] = "available", true
		} else {
			_, code, _ := NASBackupErrorStatus(err)
			result["code"] = code
		}
		_ = encoder.Encode(result)
		return err
	case "nas-backup-one":
		var request struct {
			ExpectedRevision int    `json:"expectedRevision"`
			RecordingID      string `json:"recordingID"`
		}
		if err := nasCLIDecode(input, &request); err != nil {
			return err
		}
		if _, err := StartNASBackup(request.ExpectedRevision, request.RecordingID); err != nil {
			return err
		}
		nasBackups.mu.Lock()
		done, id := nasBackups.done, nasBackups.job.ID
		nasBackups.mu.Unlock()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_, _ = CancelNASBackup(id)
				timer := time.NewTimer(15 * time.Second)
				select {
				case <-done:
					timer.Stop()
				case <-timer.C:
				}
				return ctx.Err()
			case <-ticker.C:
				nasBackups.mu.Lock()
				job := *nasBackups.job
				nasBackups.mu.Unlock()
				if encoder.Encode(map[string]any{"status": job.Status, "active": job.Active, "progress": job.Progress}) != nil {
					_, _ = CancelNASBackup(id)
				}
			case <-done:
				nasBackups.mu.Lock()
				job := *nasBackups.job
				nasBackups.mu.Unlock()
				result := map[string]any{"status": job.Status, "active": false, "verified": job.Status == "complete" || job.Status == "already-complete"}
				if job.Result != nil {
					result["sha256"], result["bytes"], result["resumedBytes"] = job.Result.SHA256, job.Result.Bytes, job.Result.ResumedBytes
				}
				if job.Code != "" {
					result["code"] = job.Code
				}
				_ = encoder.Encode(result)
				if job.Status != "complete" && job.Status != "already-complete" {
					return errors.New(job.Code)
				}
				return nil
			}
		}
	default:
		return errors.New("NAS_CLI_MODE_INVALID")
	}
}
