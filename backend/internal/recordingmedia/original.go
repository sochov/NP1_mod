package recordingmedia

import (
	"encoding/binary"
	"errors"
	"io"
)

// ConfirmOriginal requires finalized metadata and actual H.264/AAC decoder
// configurations. Codec labels or a filename alone are insufficient.
func ConfirmOriginal(r io.ReaderAt, size int64) (Metadata, error) {
	meta, err := ReadMetadata(r, size)
	if err != nil || (meta.VideoCodec != "avc1" && meta.VideoCodec != "avc3") || meta.AudioCodec != "mp4a" || !meta.avcConfig || !meta.aacConfig || meta.AudioChannels < 1 || meta.AudioChannels > 2 || meta.AudioSampleRate < 8000 || meta.AudioSampleRate > 96000 {
		return Metadata{}, errors.New("ORIGINAL_H264_AAC_UNCONFIRMED")
	}
	return meta, nil
}

func originalCodecConfig(r io.ReaderAt, start, end int64, wanted string) bool {
	for count := 0; start < end && count < 32; count++ {
		box, err := readMP4Box(r, start, end)
		if err != nil {
			return false
		}
		start = box.end
		if box.kind != wanted {
			continue
		}
		length := box.end - box.payload
		if length < 7 || length > 65536 {
			return false
		}
		data, err := readMP4Bytes(r, box, int(length))
		if err != nil {
			return false
		}
		if wanted == "avcC" {
			return originalAVC(data)
		}
		return len(data) >= 4 && originalESDescriptor(data[4:], 0)
	}
	return false
}

func originalAVC(data []byte) bool {
	if len(data) < 7 || data[0] != 1 || data[4]&3 == 2 || data[5]&31 == 0 {
		return false
	}
	offset := 6
	readNALs := func(count int, kind byte) bool {
		for i := 0; i < count; i++ {
			if offset+2 > len(data) {
				return false
			}
			length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if length < 1 || offset+length > len(data) || data[offset]&31 != kind {
				return false
			}
			offset += length
		}
		return true
	}
	if !readNALs(int(data[5]&31), 7) || offset >= len(data) || data[offset] == 0 {
		return false
	}
	count := int(data[offset])
	offset++
	return readNALs(count, 8)
}

func originalESDescriptor(data []byte, depth int) bool {
	if depth > 4 {
		return false
	}
	for count := 0; len(data) > 0 && count < 32; count++ {
		tag, offset, length := data[0], 1, 0
		for n := 0; ; n++ {
			if n >= 4 || offset >= len(data) {
				return false
			}
			v := data[offset]
			offset++
			length = (length << 7) | int(v&127)
			if v&128 == 0 {
				break
			}
		}
		if length > len(data)-offset {
			return false
		}
		payload := data[offset : offset+length]
		data = data[offset+length:]
		switch tag {
		case 3:
			if len(payload) < 3 {
				return false
			}
			next := 3
			if payload[2]&128 != 0 {
				next += 2
			}
			if payload[2]&64 != 0 {
				if next >= len(payload) {
					return false
				}
				next += 1 + int(payload[next])
			}
			if payload[2]&32 != 0 {
				next += 2
			}
			return next <= len(payload) && originalESDescriptor(payload[next:], depth+1)
		case 4:
			if len(payload) < 13 || payload[0] != 0x40 || payload[1]>>2 != 5 {
				return false
			}
			return originalESDescriptor(payload[13:], depth+1)
		case 5:
			if depth < 2 || len(payload) < 2 {
				return false
			}
			objectType := payload[0] >> 3
			frequency := (payload[0]&7)<<1 | payload[1]>>7
			channels := (payload[1] >> 3) & 15
			return (objectType == 2 || objectType == 5 || objectType == 29) && frequency < 13 && channels >= 1 && channels <= 2
		}
	}
	return false
}
