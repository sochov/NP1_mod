//go:build !linux

package adapters

import "errors"

func apLock(string) (func(), error) { return nil, errors.New("AP_LINUX_REQUIRED") }
