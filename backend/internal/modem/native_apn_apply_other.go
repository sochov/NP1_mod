//go:build !linux

package modem

import (
	"context"
	"errors"
)

func nativeAPNConnectProfile1(context.Context, savedAPNProfile) (NativeAPNStatus, error) {
	return NativeAPNStatus{}, errors.New("native APN connect requires Linux")
}
