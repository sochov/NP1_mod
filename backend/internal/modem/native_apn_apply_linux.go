//go:build linux

package modem

import "context"

func nativeAPNConnectProfile1(ctx context.Context, profile savedAPNProfile) (NativeAPNStatus, error) {
	return nativeAPNRPC(ctx, &profile)
}
