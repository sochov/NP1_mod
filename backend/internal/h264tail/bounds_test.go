package h264tail

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func finalizedTree(t *testing.T, data []byte) []byte {
	t.Helper()
	boxes, err := children(bytes.NewReader(data), 0, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, b := range boxes {
		switch b.kind {
		case "moov", "trak", "mdia", "minf", "stbl":
			out = append(out, atom(b.kind, finalizedTree(t, data[b.payload:b.end]))...)
		case "stts":
			out = append(out, full("stts", 0, 0, be32(1), be32(3), be32(100))...)
		default:
			out = append(out, atom(b.kind, data[b.payload:b.end])...)
		}
	}
	return out
}

type measuredReader struct {
	io.ReaderAt
	read int
}

func (m *measuredReader) ReadAt(b []byte, off int64) (int, error) {
	m.read += len(b)
	return m.ReaderAt.ReadAt(b, off)
}
func TestFinalizedMetadataSkipsMdat(t *testing.T) {
	c := fixture(t)
	init, err := c.InitSegment()
	if err != nil {
		t.Fatal(err)
	}
	reference := finalizedTree(t, init)
	reference = append(reference, atom("mdat", make([]byte, 1<<20))...)
	r := &measuredReader{ReaderAt: bytes.NewReader(reference)}
	got, err := ConfigFromMP4(r, int64(len(reference)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 32 || got.Height != 32 || got.SampleDuration != 100 || !bytes.Equal(got.AVC, c.AVC) {
		t.Fatal("metadata mismatch")
	}
	if r.read > 16384 {
		t.Fatal("read mdat instead of skipping it")
	}
	codec, err := got.Codec()
	if err != nil || codec != "avc1.42c028" {
		t.Fatalf("codec %q %v", codec, err)
	}
	offset, err := MediaDataOffset(bytes.NewReader(reference), int64(len(reference)))
	if err != nil || offset != int64(len(reference)-(1<<20)) {
		t.Fatal("mdat offset mismatch")
	}
	start, end, open, err := MediaDataRange(bytes.NewReader(reference), int64(len(reference)))
	if err != nil || start != offset || end != int64(len(reference)) || open {
		t.Fatal("finalized extent mismatch")
	}
	for _, bad := range [][]byte{reference[:10], []byte{0, 0, 0, 1, 'm', 'o', 'o', 'v', 255, 255, 255, 255, 255, 255, 255, 255}, reference[:len(reference)-4]} {
		if _, err := ConfigFromMP4(bytes.NewReader(bad), int64(len(bad))); err == nil {
			t.Fatal("truncated/overflow box accepted")
		}
	}
	live := join(atom("ftyp", []byte("isom")), []byte{0, 0, 0, 0, 'm', 'd', 'a', 't'}, make([]byte, 16))
	if _, err := ConfigFromMP4(bytes.NewReader(live), int64(len(live))); err == nil {
		t.Fatal("unfinalized file accepted as reference")
	}
	if offset, err := MediaDataOffset(bytes.NewReader(live), int64(len(live))); err != nil || offset != 20 {
		t.Fatal("size-zero growing mdat")
	}
	if start, end, open, err := MediaDataRange(bytes.NewReader(live), int64(len(live))); err != nil || start != 20 || end != int64(len(live)) || !open {
		t.Fatal("growing extent mismatch")
	}
}
func TestFrameGapAndPaddingValidation(t *testing.T) {
	c := fixture(t)
	d, _ := NewDemuxer(c)
	stream := append(au(c, 0, true), au(c, 2, false)...)
	stream = append(stream, au(c, 3, false)...)
	stream = append(stream, au(c, 0, true)...)
	stream = append(stream, []byte{0, 0, 0, 2, 9, 0x10}...)
	samples, err := d.Feed(stream)
	if err != nil || len(samples) != 2 || d.Dropped != 2 {
		t.Fatal("frame gap did not require IDR resync")
	}
	value := au(c, 1, false)
	n := binary.BigEndian.Uint32(value[6:10])
	binary.BigEndian.PutUint32(value[6:10], n+8)
	value = append(value, make([]byte, 8)...)
	sample, _, ok := unit(value, c)
	if !ok || !bytes.Equal(sample.Data, value) {
		t.Fatal("valid trailing_zero_8bits was modified")
	}
	value = append(value, make([]byte, 33)...)
	binary.BigEndian.PutUint32(value[6:10], n+41)
	if _, _, ok := unit(value, c); ok {
		t.Fatal("unbounded zero padding accepted")
	}
	malformed := au(c, 1, false)
	malformed = append(malformed, 0, 0, 0, 1)
	binary.BigEndian.PutUint32(malformed[6:10], uint32(len(malformed)-10))
	if _, _, ok := unit(malformed, c); ok {
		t.Fatal("interior unescaped start-code accepted")
	}
	good, _, _ := unit(au(c, 0, true), c)
	bad := good
	bad.Data = append(bytes.Clone(good.Data), 0xee, 0xee, 0xee, 0xee, 0xee)
	if _, err := c.Fragment(1, 0, []Sample{bad}); err == nil {
		t.Fatal("audio bytes accepted by public remux API")
	}
	bad = good
	bad.Keyframe = false
	if _, err := c.Fragment(1, 0, []Sample{bad}); err == nil {
		t.Fatal("forged sample flags accepted")
	}
}
func FuzzIncrementalFeed(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, 9, 16})
	f.Add([]byte{0, 0, 0, 2, 9, 16, 255, 255, 255, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		c := fixture(t)
		d, _ := NewDemuxer(c)
		for at := 0; at < len(data); at += 257 {
			samples, err := d.Feed(data[at:min(at+257, len(data))])
			if err != nil {
				t.Fatal(err)
			}
			if len(samples) > 0 {
				if _, err := c.Fragment(1, 0, samples); err != nil {
					t.Fatal("demux returned unremuxable sample", err)
				}
			}
		}
	})
}
