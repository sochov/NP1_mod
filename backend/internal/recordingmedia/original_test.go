package recordingmedia

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func originalBox(kind string, payload ...[]byte) []byte {
	size := 8
	for _, value := range payload {
		size += len(value)
	}
	out := make([]byte, 8, size)
	binary.BigEndian.PutUint32(out, uint32(size))
	copy(out[4:], kind)
	for _, value := range payload {
		out = append(out, value...)
	}
	return out
}
func originalFixture() []byte {
	movie := make([]byte, 20)
	binary.BigEndian.PutUint32(movie[12:], 1000)
	binary.BigEndian.PutUint32(movie[16:], 60000)
	track := func(audio bool) []byte {
		handler := make([]byte, 12)
		entry := make([]byte, 86)
		copy(handler[8:], "vide")
		copy(entry[4:], "avc1")
		binary.BigEndian.PutUint16(entry[32:], 1920)
		binary.BigEndian.PutUint16(entry[34:], 1080)
		entry = append(entry, originalBox("avcC", []byte{1, 100, 0, 40, 255, 225, 0, 2, 0x67, 0, 1, 0, 2, 0x68, 0})...)
		if audio {
			copy(handler[8:], "soun")
			entry = make([]byte, 36)
			copy(entry[4:], "mp4a")
			binary.BigEndian.PutUint16(entry[24:], 2)
			binary.BigEndian.PutUint32(entry[32:], 48000<<16)
			config := make([]byte, 13)
			config[0], config[1] = 0x40, 0x15
			config = append(config, 5, 2, 0x11, 0x90)
			es := append([]byte{0, 1, 0, 4, byte(len(config))}, config...)
			entry = append(entry, originalBox("esds", append([]byte{0, 0, 0, 0, 3, byte(len(es))}, es...))...)
		}
		binary.BigEndian.PutUint32(entry, uint32(len(entry)))
		return originalBox("trak", originalBox("mdia", originalBox("hdlr", handler), originalBox("minf", originalBox("stbl", originalBox("stsd", []byte{0, 0, 0, 0, 0, 0, 0, 1}, entry)))))
	}
	return bytes.Join([][]byte{originalBox("ftyp", []byte("isom0000")), originalBox("mdat", []byte("synthetic configuration fixture")), originalBox("moov", originalBox("mvhd", movie), track(false), track(true))}, nil)
}

func TestConfirmOriginalRequiresActualDecoderConfigurations(t *testing.T) {
	valid := originalFixture()
	if _, err := ConfirmOriginal(bytes.NewReader(valid), int64(len(valid))); err != nil {
		t.Fatal("valid H264/AAC configuration rejected", err)
	}
	for _, kind := range []string{"avcC", "esds", "avc1", "mp4a"} {
		bad := bytes.Replace(valid, []byte(kind), []byte("xxxx"), 1)
		if _, err := ConfirmOriginal(bytes.NewReader(bad), int64(len(bad))); err == nil {
			t.Fatal("missing configuration accepted", kind)
		}
	}
	if originalESDescriptor([]byte{3, 0x80, 0x80, 0x80, 0x80, 0x80}, 0) || originalAVC([]byte{1, 100, 0, 0, 255, 225, 255, 255}) {
		t.Fatal("malformed descriptor accepted")
	}
}

func TestConfirmPrivateOriginalWhenProvided(t *testing.T) {
	filename := os.Getenv("NCC_TEST_ORIGINAL_MP4")
	if filename == "" {
		t.Skip("private finalized recording not supplied")
	}
	file, err := os.Open(filename)
	if err != nil {
		t.Fatal("private original could not be opened")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal("private original metadata unavailable")
	}
	if _, err = ConfirmOriginal(file, info.Size()); err != nil {
		t.Fatal("private original H264/AAC configuration not confirmed")
	}
}
