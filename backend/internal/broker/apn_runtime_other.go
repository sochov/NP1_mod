//go:build !linux

package broker

import "os"

func apnRuntimeNoFollow() int                 { return 0 }
func trustedAPNRuntime(info os.FileInfo) bool { return info.Mode().IsRegular() }
