//go:build !linux

package modem

import (
	"context"
	"errors"
)

func ReadGobiNetAPN(context.Context) (APNObservation, error) {
	return APNObservation{}, errors.New("WDS observation requires the verified NP1 Linux driver")
}
