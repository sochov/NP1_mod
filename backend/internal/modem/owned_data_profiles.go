package modem

import "bytes"

// Compare every captured TLV (including all unsupported original fields), the
// protocol error establishing absence, and the same-CID inventory. This is not
// merely a comparison of the five fields understood by the APN editor.
func ownedProfilesIdentical(before, after ProfileRecovery) bool {
	if !bytes.Equal(before.inventoryFrame, after.inventoryFrame) {
		return false
	}
	for i := range before.profiles {
		if before.profiles[i].index != after.profiles[i].index || before.profiles[i].status != after.profiles[i].status || !bytes.Equal(before.profiles[i].raw, after.profiles[i].raw) {
			return false
		}
	}
	return true
}
