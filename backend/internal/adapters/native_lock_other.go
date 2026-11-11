//go:build !linux

package adapters

import "errors"

func lockNativeControl() (func(), error) { return nil, errors.New("native controls require Linux") }
