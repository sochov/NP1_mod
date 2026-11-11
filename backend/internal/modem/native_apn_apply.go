package modem

import (
	"context"
	"encoding/binary"
	"errors"
)

// nativeAPNProfile1Request is intentionally private: neither a browser nor a
// command-line caller can select command bytes or include profile2.
func nativeAPNProfile1Request(profile savedAPNProfile) ([]byte, error) {
	if profile.index != 1 || !profile.nativeFieldsComplete() {
		return nil, errors.New("native profile1 input incomplete or unsupported")
	}
	raw := make([]byte, 640)
	binary.LittleEndian.PutUint32(raw, 1)
	binary.LittleEndian.PutUint32(raw[8:], 0x0b)
	binary.LittleEndian.PutUint32(raw[12:], 0x14)
	raw[24] = 1
	for _, field := range []struct {
		offset int
		value  string
	}{{26, *profile.apn}, {126, *profile.username}, {226, *profile.password}} {
		for i, c := range []byte(field.value) {
			binary.LittleEndian.PutUint16(raw[field.offset+i*2:], uint16(c))
		}
	}
	binary.LittleEndian.PutUint32(raw[328:], uint32(*profile.authentication))
	return raw, nil // Client*=NULL and all profile2 bytes (332..639) remain zero.
}

type NativeAPNSameProfileTrial = NativeAPNApplyResult

func sameNativeProfile1(a, b savedAPNProfile) bool {
	return a.index == 1 && b.index == 1 && a.nativeFieldsComplete() && b.nativeFieldsComplete() && *a.pdp == *b.pdp && *a.apn == *b.apn && *a.username == *b.username && *a.password == *b.password && *a.authentication == *b.authentication
}
func nativeAPNOnlyProfile1(inventory ProfileInventory) bool {
	return len(inventory.Indices) == 1 && inventory.Indices[0] == 1 && inventory.Profile1Present && !inventory.Profile2Present && !inventory.Current
}

// TrialNativeAPNSameProfile1 has no arbitrary input. It reads and privately
// backs up the current saved profile, proves profile2 is absent, then sends
// exactly those profile1 values through the existing stock owner. It does not
// enable general Web apply. Its caller must stop only our independent modem
// observer to obtain the WDS read-backs, and enforce an external timeout.
func TrialNativeAPNSameProfile1(parent context.Context) (NativeAPNSameProfileTrial, error) {
	return nativeAPNApply(parent, NativeAPNProfile1Input{}, true)
}
