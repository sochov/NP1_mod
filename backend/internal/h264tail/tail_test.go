package h264tail

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type bitWriter struct {
	data []byte
	at   int
}

func (w *bitWriter) put(v uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.at%8 == 0 {
			w.data = append(w.data, 0)
		}
		w.data[w.at/8] |= byte((v>>uint(i))&1) << uint(7-w.at%8)
		w.at++
	}
}
func (w *bitWriter) ue(v uint32) {
	v++
	n := 0
	for x := v; x > 1; x >>= 1 {
		n++
	}
	w.put(0, n)
	w.put(v, n+1)
}
func (w *bitWriter) finish() []byte {
	w.put(1, 1)
	for w.at%8 != 0 {
		w.put(0, 1)
	}
	return w.data
}
func fixture(t *testing.T) Config {
	t.Helper()
	s := bitWriter{}
	s.put(66, 8)
	s.put(0xc0, 8)
	s.put(40, 8)
	s.ue(0)
	s.ue(0)
	s.ue(2)
	s.ue(1)
	s.put(0, 1)
	s.ue(1)
	s.ue(1)
	s.put(1, 1)
	s.put(1, 1)
	s.put(0, 1)
	s.put(0, 1)
	sps := append([]byte{0x67}, s.finish()...)
	p := bitWriter{}
	p.ue(0)
	p.ue(0)
	p.put(0, 1)
	p.put(0, 1)
	p.ue(0)
	p.ue(0)
	p.ue(0)
	p.put(0, 1)
	p.put(0, 2)
	p.ue(0)
	p.ue(0)
	p.ue(0)
	p.put(1, 1)
	p.put(0, 1)
	p.put(0, 1)
	pps := append([]byte{0x68}, p.finish()...)
	avc := []byte{1, 66, 0xc0, 40, 0xff, 0xe1, 0, byte(len(sps))}
	avc = append(avc, sps...)
	avc = append(avc, 1, 0, byte(len(pps)))
	avc = append(avc, pps...)
	c, err := (Config{AVC: avc, Timescale: 2700, SampleDuration: 100, Width: 32, Height: 32}).validated()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func nal(b []byte) []byte { return append(be32(uint32(len(b))), b...) }
func au(c Config, frame uint32, key bool) []byte {
	b := []byte{0, 0, 0, 2, 9, 0x10}
	if key {
		b = append(b, nal(c.sps)...)
		b = append(b, nal(c.pps)...)
	}
	w := bitWriter{}
	w.ue(0)
	if key {
		w.ue(2)
	} else {
		w.ue(0)
	}
	w.ue(0)
	w.put(frame, 4)
	header := byte(0x41)
	if key {
		header = 0x65
		w.ue(0)
	}
	b = append(b, nal(append([]byte{header}, w.finish()...))...)
	return b
}

func TestIncrementalAVCCOnly(t *testing.T) {
	c := fixture(t)
	stream := append(au(c, 0, true), bytes.Repeat([]byte{0xee}, 17)...)
	stream = append(stream, au(c, 1, false)...)
	stream = append(stream, bytes.Repeat([]byte{0xdd}, 29)...)
	stream = append(stream, au(c, 2, false)...)
	stream = append(stream, []byte{0, 0, 0, 2, 9, 0x10}...)
	for chunk := 1; chunk <= 17; chunk++ {
		d, _ := NewDemuxer(c)
		var got []Sample
		for start := 0; start < len(stream); start += chunk {
			end := min(start+chunk, len(stream))
			samples, err := d.Feed(stream[start:end])
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, samples...)
		}
		if len(got) != 3 || !got[0].Keyframe || got[1].FrameNumber != 1 || !bytes.Equal(got[2].Data, au(c, 2, false)) {
			t.Fatalf("chunk=%d got=%+v dropped=%d", chunk, got, d.Dropped)
		}
	}
}
func TestRequiresIDRAndMatchingParameters(t *testing.T) {
	c := fixture(t)
	d, _ := NewDemuxer(c)
	stream := append(au(c, 1, false), au(c, 0, true)...)
	bad := au(c, 1, false)
	bad[6+4] = 0x81
	stream = append(stream, bad...)
	stream = append(stream, au(c, 2, false)...)
	stream = append(stream, au(c, 0, true)...)
	stream = append(stream, []byte{0, 0, 0, 2, 9, 0x10}...)
	got, err := d.Feed(stream)
	if err != nil || len(got) != 2 || !got[0].Keyframe || !got[1].Keyframe {
		t.Fatalf("unexpected resync: %v %+v", err, got)
	}
	wrong := au(c, 0, true)
	wrong[11] ^= 1
	d, _ = NewDemuxer(c)
	got, err = d.Feed(append(wrong, au(c, 1, false)...))
	if err != nil || len(got) != 0 {
		t.Fatal("different SPS accepted")
	}
}
func TestFalseAUDAndHugeLengthsCannotProduceAudioSample(t *testing.T) {
	c := fixture(t)
	d, _ := NewDemuxer(c)
	fake := append([]byte{0, 0, 0, 2, 9, 0x10}, []byte{0, 0, 0, 6, 0x41, 0, 0, 0, 0, 0}...)
	stream := append(au(c, 0, true), fake...)
	stream = append(stream, au(c, 0, true)...)
	stream = append(stream, []byte{0, 0, 0, 2, 9, 0x10}...)
	got, err := d.Feed(stream)
	if err != nil || len(got) != 2 {
		t.Fatalf("unexpected audio candidate acceptance %v %+v", err, got)
	}
	for _, sample := range got {
		if !sample.Keyframe || !bytes.Equal(sample.Data, au(c, 0, true)) {
			t.Fatal("AAC-like payload escaped video gate")
		}
	}
	_, err = d.Feed(make([]byte, maxBuffered+1))
	if err == nil {
		t.Fatal("unbounded input accepted")
	}
}
func TestConfigurationAndFragmentStructure(t *testing.T) {
	c := fixture(t)
	init, err := c.InitSegment()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ConfigFromMP4(bytes.NewReader(init), int64(len(init)))
	if err == nil {
		t.Fatal("initialization segment without real stts must not be a reference recording")
	}
	_ = parsed
	sample, _, ok := unit(au(c, 0, true), c)
	if !ok {
		t.Fatal("fixture AU")
	}
	frag, err := c.Fragment(1, 2700, []Sample{sample})
	if err != nil {
		t.Fatal(err)
	}
	top, err := children(bytes.NewReader(frag), 0, int64(len(frag)))
	if err != nil || len(top) != 2 || top[0].kind != "moof" || top[1].kind != "mdat" {
		t.Fatalf("fragment boxes: %+v %v", top, err)
	}
	if !bytes.Equal(frag[top[1].payload:], sample.Data) {
		t.Fatal("remux changed compressed video")
	}
	at := bytes.Index(frag, []byte("trun"))
	if at < 0 || binary.BigEndian.Uint32(frag[at+12:at+16]) != uint32(top[1].payload) {
		t.Fatal("trun data offset mismatch")
	}
	for _, bad := range []Config{{}, {AVC: c.AVC, Timescale: 2700, SampleDuration: 0, Width: 32, Height: 32}} {
		if _, err := bad.InitSegment(); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if _, err := c.Fragment(1, ^uint64(0), []Sample{sample}); err == nil {
		t.Fatal("decode timestamp overflow accepted")
	}
}

// Optional ignored real-media fixture, never a checked-in recording. The output
// can be independently decoded by ffmpeg to verify every recovered video frame.
func TestPrivateRecordingRoundTrip(t *testing.T) {
	path := os.Getenv("NCC_H264_PRIVATE_FIXTURE")
	if path == "" {
		t.Skip("private recording not requested")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	c, err := ConfigFromMP4(file, st.Size())
	if err != nil {
		t.Fatal("config", err)
	}
	offset, err := MediaDataOffset(file, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDemuxer(c)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "recovered.mp4")
	if requested := os.Getenv("NCC_H264_PRIVATE_OUTPUT"); requested != "" {
		out = requested
	}
	target, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	init, err := c.InitSegment()
	if err != nil {
		t.Fatal(err)
	}
	target.Write(init)
	buf := make([]byte, 65536)
	var frames uint64
	seq := uint32(1)
	top, err := children(file, 0, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	mdat, ok := find(top, "mdat")
	if !ok {
		t.Fatal("missing finalized mdat")
	}
	reader := io.NewSectionReader(file, offset, mdat.end-offset)
	for {
		n, readErr := reader.Read(buf)
		if readErr != nil && readErr != io.EOF {
			t.Fatal(readErr)
		}
		chunk := buf[:n]
		if readErr == io.EOF {
			chunk = append(chunk, 0, 0, 0, 2, 9, 0x10)
		}
		samples, err := d.Feed(chunk)
		if err != nil {
			t.Fatal(err)
		}
		if len(samples) > 0 {
			fragment, err := c.Fragment(seq, frames*uint64(c.SampleDuration), samples)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = target.Write(fragment); err != nil {
				t.Fatal(err)
			}
			seq++
			frames += uint64(len(samples))
		}
		if readErr == io.EOF {
			break
		}
	}
	if frames < 20 {
		t.Fatalf("too few recovered samples: %d", frames)
	}
	t.Logf("recoveredFrames=%d dropped=%d timescale=%d duration=%d", frames, d.Dropped, c.Timescale, c.SampleDuration)
}
