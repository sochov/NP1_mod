package platespeech

import (
	"encoding/binary"
	"errors"
)

// All clips are stored and played as 24 kHz mono signed 16-bit PCM, the
// format the VOICEVOX API returns; uploads are converted in the browser.
const (
	SampleRate   = 24000
	bytesPerSec  = SampleRate * 2
	MaxClipBytes = 30 * bytesPerSec
)

var errFormat = errors.New("音声は24kHz・モノラル・16bitのWAVである必要があります")

// ParseWAV returns the PCM samples of a RIFF/WAVE file in the clip format.
// Chunks other than fmt and data (LIST, fact, ...) are skipped.
func ParseWAV(b []byte) ([]byte, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errFormat
	}
	var format bool
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := off + 8
		end := body + size
		if size < 0 || end > len(b) {
			if id == "data" { // streamed WAVs may carry a placeholder size
				end = len(b)
			} else {
				return nil, errFormat
			}
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errFormat
			}
			f := b[body:end]
			if binary.LittleEndian.Uint16(f[0:2]) != 1 || binary.LittleEndian.Uint16(f[2:4]) != 1 ||
				binary.LittleEndian.Uint32(f[4:8]) != SampleRate || binary.LittleEndian.Uint16(f[14:16]) != 16 {
				return nil, errFormat
			}
			format = true
		case "data":
			if !format {
				return nil, errFormat
			}
			pcm := b[body:end]
			pcm = pcm[:len(pcm)&^1]
			if len(pcm) == 0 || len(pcm) > MaxClipBytes {
				return nil, errors.New("音声は30秒以内にしてください")
			}
			return pcm, nil
		}
		off = end + size&1
	}
	return nil, errFormat
}

// EncodeWAV wraps clip PCM in a WAV header (used for previews).
func EncodeWAV(pcm []byte) []byte {
	out := make([]byte, 44+len(pcm))
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(36+len(pcm)))
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], 1)
	binary.LittleEndian.PutUint32(out[24:], SampleRate)
	binary.LittleEndian.PutUint32(out[28:], bytesPerSec)
	binary.LittleEndian.PutUint16(out[32:], 2)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(len(pcm)))
	copy(out[44:], pcm)
	return out
}

func silence(ms int) []byte { return make([]byte, ms*bytesPerSec/1000&^1) }
