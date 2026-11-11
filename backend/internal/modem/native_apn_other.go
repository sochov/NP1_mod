//go:build !linux

package modem

import (
	"context"
	"errors"
)

func ReadNativeAPNStatus(context.Context) (NativeAPNStatus, error) {
	return NativeAPNStatus{}, errors.New("native APN status requires Linux")
}
