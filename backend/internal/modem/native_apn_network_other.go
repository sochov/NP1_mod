//go:build !linux

package modem

import (
	"context"
	"errors"
)

func captureNativeAPNNetwork(context.Context) (nativeAPNNetworkBaseline, error) {
	return nativeAPNNetworkBaseline{}, errors.New("APN_NETWORK_REQUIRES_LINUX")
}
func restoreNativeAPNDefault(context.Context, nativeAPNNetworkBaseline) (string, error) {
	return "unknown", errors.New("APN_NETWORK_REQUIRES_LINUX")
}
func nativeAPNDataLink(context.Context) (NativeAPNDataLink, error) {
	return NativeAPNDataLink{Status: "unknown"}, errors.New("APN_NETWORK_REQUIRES_LINUX")
}
func settleNativeAPNDefault(context.Context, nativeAPNNetworkBaseline) (string, error) {
	return "unknown", errors.New("APN_NETWORK_REQUIRES_LINUX")
}
