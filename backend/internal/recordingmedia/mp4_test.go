package recordingmedia

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMetadataVersion1(t *testing.T) {
	mvhd := make([]byte, 32)
	mvhd[0] = 1
	binary.BigEndian.PutUint32(mvhd[20:], 1000)
	binary.BigEndian.PutUint64(mvhd[24:], 90000)
	v, e := mp4MovieDuration(bytes.NewReader(mvhd), mp4Box{payload: 0, end: int64(len(mvhd))})
	if e != nil || v != 90 {
		t.Fatalf("v1 duration %v %v", v, e)
	}
	binary.BigEndian.PutUint32(mvhd[20:], 0)
	if _, e = mp4MovieDuration(bytes.NewReader(mvhd), mp4Box{payload: 0, end: int64(len(mvhd))}); e == nil {
		t.Fatal("zero timescale accepted")
	}
}
