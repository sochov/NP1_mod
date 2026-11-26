package platesensor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Synthetic slice headers only. No plate image or device data is embedded.
func testFrontAccessUnit() []byte {
	result := []byte{0, 0, 0, 2, 9, 0x10}
	for _, s := range []string{"6742c028da01e0089f95", "68ce3c80", "65b860"} {
		nal, _ := hex.DecodeString(s)
		result = binary.BigEndian.AppendUint32(result, uint32(len(nal)))
		result = append(result, nal...)
	}
	return result
}

func TestFrontRecordingStartsAtEOFAndRotates(t *testing.T) {
	dir := t.TempDir()
	frame := testFrontAccessUnit()
	media := append([]byte{0, 0, 0, 0, 'm', 'd', 'a', 't'}, frame...)
	old := filepath.Join(dir, "FILEM_260101_010000_0000.MP4")
	if err := os.WriteFile(old, media, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "FILEE_260101_010200_0000.MP4"), media, 0600); err != nil {
		t.Fatal(err)
	}
	tail, err := openLiveRecording(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer tail.file.Close()
	var dst bytes.Buffer
	check := func(want bool) {
		t.Helper()
		got, err := tail.readGrowth(context.Background(), &dst, make([]byte, 7), time.Now())
		if err != nil || got != want {
			t.Fatalf("growth %v, error %v", got, err)
		}
	}
	check(false)
	if dst.Len() != 0 {
		t.Fatal("read historical image")
	}
	out, err := os.OpenFile(old, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err = out.WriteAt(append(bytes.Clone(frame), frame...), int64(len(media))); err != nil {
		t.Fatal(err)
	}
	check(true)
	want := bytes.Clone(frame)
	if err = annexSample(want); err != nil || !bytes.Equal(want, dst.Bytes()) {
		t.Fatal("missing live IDR")
	}
	check(false)
	if !bytes.Equal(want, dst.Bytes()) {
		t.Fatal("repeated the same image")
	}

	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(media)+2*len(frame)))
	if _, err = out.WriteAt(length[:], 0); err != nil {
		t.Fatal(err)
	}
	if _, err = tail.readGrowth(context.Background(), &dst, make([]byte, 128), time.Now()); !errors.Is(err, io.EOF) {
		t.Fatal("failed to observe finalized mdat", err)
	}
	if _, err = openLiveRecording(dir, time.Now()); err == nil {
		t.Fatal("opened finalized or cabin recording")
	}
	newer := filepath.Join(dir, "FILEM_260101_010300_0000.MP4")
	if err = os.WriteFile(newer, media, 0600); err != nil {
		t.Fatal(err)
	}
	next, err := openLiveRecording(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer next.file.Close()
	if next.offset != int64(len(media)) {
		t.Fatal("rotation replayed old frames")
	}
}

func TestFrontRecordingDropsStaleOrTruncatedInput(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "FILEM_260101_010000_0000.MP4")
	media := append([]byte{0, 0, 0, 0, 'm', 'd', 'a', 't'}, testFrontAccessUnit()...)
	if err := os.WriteFile(file, media, 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := openLiveRecording(dir, now.Add(time.Hour)); err == nil {
		t.Fatal("opened stale recording")
	}
	live, err := openLiveRecording(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	defer live.file.Close()
	if err = os.Truncate(file, int64(len(media)-1)); err != nil {
		t.Fatal(err)
	}
	if _, err = live.readGrowth(context.Background(), io.Discard, make([]byte, 128), now); err == nil {
		t.Fatal("consumed truncated recording")
	}
}

func TestFrontRecordingBackpressureResumesAtFreshIDR(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "FILEM_260101_010000_0000.MP4")
	idr := testFrontAccessUnit()
	media := append([]byte{0, 0, 0, 0, 'm', 'd', 'a', 't'}, idr...)
	if err := os.WriteFile(name, media, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := openLiveRecording(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer r.file.Close()
	writer, err := os.OpenFile(name, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	pict := func(frame byte) []byte {
		return []byte{0, 0, 0, 2, 9, 0x10, 0, 0, 0, 2, 0x41, 0xe1 | (frame << 1)}
	}
	// Each access unit is released only after the next complete AUD.
	step := func(old time.Duration, additions ...[]byte) []byte {
		t.Helper()
		for _, data := range additions {
			if _, err := writer.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		now := time.Now()
		r.observed = now.Add(-old)
		var out bytes.Buffer
		advanced, err := r.readGrowth(context.Background(), &out, make([]byte, 128), now)
		if !advanced || err != nil {
			t.Fatalf("growth=%v, error=%v", advanced, err)
		}
		return out.Bytes()
	}
	want := func(inputs ...[]byte) []byte {
		t.Helper()
		v := bytes.Join(inputs, nil)
		if err := annexSample(v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if got := step(0, idr, pict(1)); !bytes.Equal(got, want(idr)) {
		t.Fatal("missing initial IDR")
	}
	if got := step(400*time.Millisecond, pict(2)); len(got) != 0 {
		t.Fatal("sent delayed P")
	}
	if got := step(0, idr); len(got) != 0 {
		t.Fatal("sent P referencing skipped picture")
	}
	if got := step(0, pict(1), pict(2)); !bytes.Equal(got, want(idr, pict(1))) {
		t.Fatal("failed to resume on subsequent IDR")
	}
	// At a severe stall discard the pending P2, plus all data that was
	// appended before reconnecting to the current file edge.
	if got := step(cameraFrameWait+time.Second, pict(3), idr); len(got) != 0 {
		t.Fatal("replayed backlog")
	}
	if got := step(0, pict(1), pict(2), idr, pict(1)); !bytes.Equal(got, want(idr)) {
		t.Fatal("did not resume at new post-stall IDR")
	}
}
