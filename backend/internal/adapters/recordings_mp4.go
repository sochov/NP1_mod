package adapters

import (
	"github.com/np1-control-center/ncc/internal/recordingmedia"
	"io"
)

type MP4Metadata = recordingmedia.Metadata

func ReadRecordingMetadata(r io.ReaderAt, size int64) (MP4Metadata, error) {
	return recordingmedia.ReadMetadata(r, size)
}
