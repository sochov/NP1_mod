package modem

import "errors"

// Only the fixed reader which obtains all three replies on the same independent
// WDS CID calls this. The private schema retains the validated raw list frame;
// an externally supplied public inventory summary cannot authorize absence.
func (r ProfileRecovery) withSameCIDInventory(frame []byte) (ProfileRecovery, error) {
	if _, err := parseProfileInventory(frame); err != nil {
		return ProfileRecovery{}, errors.New("profile inventory frame invalid")
	}
	r.inventoryFrame = append([]byte(nil), frame...)
	return r, nil
}

func (r ProfileRecovery) profileAbsence(index int) (bool, string) {
	if index != 1 && index != 2 {
		return false, ""
	}
	p := r.profiles[index-1]
	legacy := p.absenceConfirmed()
	if len(r.inventoryFrame) == 0 {
		if legacy {
			return true, "profile-get-error"
		}
		return false, ""
	}
	inventory, err := parseProfileInventory(r.inventoryFrame)
	if err != nil {
		return false, ""
	}
	for _, present := range inventory.Indices {
		if present == index {
			return false, ""
		}
	}
	if legacy {
		return true, "profile-get-and-same-cid-inventory"
	}
	// Extended 5 is INVALID_PROFILE_NUMBER, not sufficient on its own. It only
	// confirms profile2 absence together with the fixed 3GPP list containing
	// exactly profile1 and the successful profile1 capture in the same action.
	if index == 2 && p.status == "failed" && p.protocolError != nil && *p.protocolError == 81 && p.extendedError != nil && *p.extendedError == 5 && r.profiles[0].status == "available" && nativeAPNOnlyProfile1(inventory) {
		return true, "profile-get-and-same-cid-inventory"
	}
	return false, ""
}
