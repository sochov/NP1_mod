//go:build !linux

package modem

import (
	"context"
	"errors"
)

func ReadGobiNetSignal(context.Context) (GobiNetSignal, error) {
	return GobiNetSignal{}, errors.New("GobiNet observation requires the verified NP1 Linux driver")
}
func ReadGobiNetStatus(context.Context) (GobiNetSignal, error) {
	return GobiNetSignal{}, errors.New("GobiNet status observation requires the verified NP1 Linux driver")
}
