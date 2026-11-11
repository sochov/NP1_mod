//go:build !linux

package adapters

import "errors"

func lockNativeControl() (func(), error) { return nil, errors.New("native controls require Linux") }
func lockNativeControlExisting() (func(), error) {
	return nil, errors.New("native controls require Linux")
}
func InitializeNativeControlLock() error { return errors.New("native controls require Linux") }
