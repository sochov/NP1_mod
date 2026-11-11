package nasbackup

import (
	"context"
	"io"
)

type RemoteInfo struct {
	Size     int64
	Regular  bool
	Identity string
}
type Part interface {
	io.WriteSeeker
	Sync() error
	Stat() (RemoteInfo, error)
	Close() error
}

// Paths are single generated basenames. A concrete Remote is permanently
// scoped to one configured directory; no delete or arbitrary listing exists.
type Remote interface {
	TargetKey() string
	Stat(context.Context, string) (RemoteInfo, error)
	Open(context.Context, string) (io.ReadCloser, error)
	OpenPart(context.Context, string, bool) (Part, error)
	RenameNoReplace(context.Context, string, string) error
}
