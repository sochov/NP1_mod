//go:build !linux

package modem

import "os"

// Non-Linux is used only for local parser/persistence tests; RunWorker rejects
// execution outside its fixed Linux runtime path.
func trustedWorkerFile(st os.FileInfo) bool { return st.Mode().IsRegular() }
