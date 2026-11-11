package adapters

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recordingBox(name string, payload ...[]byte) []byte {
	size := 8
	for _, b := range payload {
		size += len(b)
	}
	out := make([]byte, 8, size)
	binary.BigEndian.PutUint32(out[:4], uint32(size))
	copy(out[4:8], name)
	for _, b := range payload {
		out = append(out, b...)
	}
	return out
}
func recordingFixture(seconds uint32) []byte {
	mvhd := make([]byte, 20)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	binary.BigEndian.PutUint32(mvhd[16:], seconds*1000)
	track := func(kind, codec string) []byte {
		handler := make([]byte, 12)
		copy(handler[8:], kind)
		entry := make([]byte, 86)
		binary.BigEndian.PutUint32(entry[:4], uint32(len(entry)))
		copy(entry[4:8], codec)
		if kind == "vide" {
			binary.BigEndian.PutUint16(entry[32:], 1920)
			binary.BigEndian.PutUint16(entry[34:], 1080)
		} else {
			binary.BigEndian.PutUint16(entry[24:], 2)
			binary.BigEndian.PutUint32(entry[32:], 48000<<16)
		}
		stsd := make([]byte, 8)
		binary.BigEndian.PutUint32(stsd[4:], 1)
		return recordingBox("trak", recordingBox("mdia", recordingBox("hdlr", handler), recordingBox("minf", recordingBox("stbl", recordingBox("stsd", stsd, entry)))))
	}
	return bytes.Join([][]byte{recordingBox("ftyp", []byte("isom0000")), recordingBox("mdat", []byte("synthetic metadata fixture, not video")), recordingBox("moov", recordingBox("mvhd", mvhd), track("vide", "avc1"), track("soun", "mp4a"))}, nil)
}
func TestRecordingMetadataBoundaries(t *testing.T) {
	valid := recordingFixture(180)
	meta, e := ReadRecordingMetadata(bytes.NewReader(valid), int64(len(valid)))
	if e != nil {
		t.Fatal(e)
	}
	if meta.DurationSeconds != 180 || meta.Width != 1920 || meta.Height != 1080 || meta.VideoCodec != "avc1" || meta.AudioCodec != "mp4a" || meta.AudioChannels != 2 || meta.AudioSampleRate != 48000 {
		t.Fatal(meta)
	}
	for _, bad := range [][]byte{valid[:len(valid)-4], recordingBox("ftyp", []byte("isom0000")), recordingBox("mdat", []byte("not finalized")), {0, 0, 0, 4, 'm', 'o', 'o', 'v'}, {0xff, 0xff, 0xff, 0xff, 'm', 'o', 'o', 'v'}} {
		if _, e := ReadRecordingMetadata(bytes.NewReader(bad), int64(len(bad))); e == nil {
			t.Fatal("malformed/unfinalized recording accepted")
		}
	}
}
func TestRecordingIndexJSTDaysAndCardIdentity(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "DRIVERECDATA", "Video")
	os.MkdirAll(dir, 0700)
	for _, name := range []string{"FILEM_261111_235900_0000.MP4", "FILEE_261111_235900_0000.MP4"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, recordingFixture(180), 0600)
		old := time.Now().Add(-time.Minute)
		os.Chtimes(p, old, old)
	}
	token := "card-a"
	index := NewRecordingIndex(base, func() (string, error) { return token, nil })
	catalog, e := index.Catalog(context.Background(), "2026-11-12")
	if e != nil || len(catalog.Clips) != 2 || len(catalog.Days) != 2 {
		t.Fatalf("catalog: %+v %v", catalog, e)
	}
	if catalog.Days[0].Date != "2026-11-12" || catalog.Days[0].RecordedSeconds != 120 || catalog.Days[1].RecordedSeconds != 60 {
		t.Fatal(catalog.Days)
	}
	if catalog.Clips[0].StartAt.Format(time.RFC3339) != "2026-11-11T14:59:00Z" {
		t.Fatal("filename did not use JST")
	}
	id := catalog.Clips[0].ID
	if _, e := recordingPath(id, token); e != nil {
		t.Fatal(e)
	}
	token = "card-b"
	catalog, e = index.Catalog(context.Background(), "2026-11-11")
	if e != nil {
		t.Fatal(e)
	}
	if catalog.Clips[0].ID == id {
		t.Fatal("SD exchange reused old ID")
	}
	if _, e := recordingPath(id, token); e == nil {
		t.Fatal("old SD recording ID accepted")
	}
	if _, e := index.Catalog(context.Background(), "2026-02-30"); e == nil {
		t.Fatal("invalid date accepted")
	}
}
func TestRecordingIDRejectsOtherFiles(t *testing.T) {
	for _, relative := range []string{"etc/passwd", "DRIVERECDATA/Video/unrelated.MP4", "DRIVERECDATA/Other/FILEM_261111_120000_0000.MP4", "DRIVERECDATA/Event/../Video/FILEM_261111_120000_0000.MP4"} {
		if _, e := recordingPath(recordingID("card", relative), "card"); e == nil {
			t.Fatalf("unexpected path accepted: %s", relative)
		}
	}
	if _, _, ok := parseRecordingFilename("FILEM_260230_120000_0000.MP4"); ok {
		t.Fatal("invalid filename date accepted")
	}
}
func TestRecordingIndexEventParkingAndPending(t *testing.T) {
	base := t.TempDir()
	for _, dir := range []string{"Event", "Parking"} {
		folder := filepath.Join(base, "DRIVERECDATA", dir)
		os.MkdirAll(folder, 0700)
		p := filepath.Join(folder, "FILEM_261111_120000_0000.MP4")
		os.WriteFile(p, recordingFixture(60), 0600)
		old := time.Now().Add(-time.Minute)
		os.Chtimes(p, old, old)
	}
	folder := filepath.Join(base, "DRIVERECDATA", "Video")
	os.MkdirAll(folder, 0700)
	os.WriteFile(filepath.Join(folder, "FILEM_261111_121000_0000.MP4"), recordingFixture(60), 0600)
	catalog, e := NewRecordingIndex(base, func() (string, error) { return "card", nil }).Catalog(context.Background(), "")
	if e != nil || len(catalog.Clips) != 2 || catalog.PendingFiles != 1 || catalog.Days[0].RecordedSeconds != 60 {
		t.Fatalf("%+v %v", catalog, e)
	}
	for _, clip := range catalog.Clips {
		if clip.Kind != "event" && clip.Kind != "parking" {
			t.Fatal(clip)
		}
		if _, e := recordingPath(clip.ID, "card"); e != nil {
			t.Fatal(e)
		}
	}
}
func TestPrivateRecordingMetadataWhenAvailable(t *testing.T) {
	found := false
	for _, name := range []string{"sample-M.mp4", "sample-E.mp4"} {
		path := filepath.Join("..", "..", "..", "tmp", name)
		f, e := os.Open(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		found = true
		st, e := f.Stat()
		if e != nil {
			f.Close()
			t.Fatal(e)
		}
		meta, e := ReadRecordingMetadata(f, st.Size())
		f.Close()
		if e != nil {
			t.Fatal(e)
		}
		if meta.Width != 1920 || meta.Height != 1080 || meta.VideoCodec != "avc1" || meta.AudioCodec != "mp4a" || meta.AudioSampleRate != 48000 || meta.AudioChannels != 2 || meta.DurationSeconds < 1 {
			t.Fatalf("unexpected private sample metadata %+v", meta)
		}
		if strings.Contains(meta.VideoCodec, "/") {
			t.Fatal("invalid codec")
		}
	}
	if !found {
		t.Skip("private real-device sample not present; synthetic tests remain active")
	}
}
