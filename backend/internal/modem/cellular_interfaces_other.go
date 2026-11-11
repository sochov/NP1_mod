//go:build !linux

package modem

import "errors"

func VerifyCellularInterfaces() ([]string, error) {
	return nil, errors.New("CELLULAR_VERIFICATION_REQUIRES_LINUX")
}
