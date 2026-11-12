package platespeech

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"time"
)

// Pacat plays clip PCM on the device speaker through the stock PulseAudio
// "primary" sink, the same path as the browser screen-audio feature. Only
// the stream volume is set; the system sink volume is never changed.
type Pacat struct{ Path string }

func (p Pacat) Available() error {
	if _, e := exec.LookPath(p.Path); e != nil {
		return errors.New("本体の音声出力（PulseAudio）を利用できません")
	}
	return nil
}

func (p Pacat) Play(ctx context.Context, pcm []byte, volume int) error {
	if volume < 0 || volume > 100 {
		return errors.New("invalid volume")
	}
	seconds := time.Duration(len(pcm)/bytesPerSec+1) * time.Second
	ctx, cancel := context.WithTimeout(ctx, seconds+10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.Path, "--playback", "--raw", "--server=unix:/run/pulse/native", "--device=primary",
		"--client-name=NP1-Plate-Speech", "--stream-name=Plate-announcement", "--format=s16le", "--rate=24000",
		"--channels=1", "--latency-msec=150", "--volume="+strconv.Itoa(volume*65536/100))
	cmd.Stdin = bytes.NewReader(pcm)
	cmd.WaitDelay = time.Second
	if out, e := cmd.CombinedOutput(); e != nil {
		if len(out) > 200 {
			out = out[:200]
		}
		return errors.New("本体スピーカーで再生できませんでした: " + string(bytes.TrimSpace(out)))
	}
	return nil
}
