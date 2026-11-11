package modem

import (
	"errors"
	"sort"
	"time"
)

type ProfileInventory struct {
	ObservedAt      time.Time `json:"observedAt"`
	Source          string    `json:"source"`
	Current         bool      `json:"current"`
	ProfileType     string    `json:"profileType"`
	Indices         []int     `json:"indices"`
	Profile1Present bool      `json:"profile1Present"`
	Profile2Present bool      `json:"profile2Present"`
}

func profileInventoryRequest() []byte { return []byte{0, 7, 0, 0x2a, 0, 4, 0, 0x10, 1, 0, 0} }
func parseProfileInventory(frame []byte) (ProfileInventory, error) {
	values, e := parseQMIResponse(frame, 7, 0x2a)
	if e != nil {
		return ProfileInventory{}, e
	}
	list, ok := values[1]
	if !ok || len(list) < 1 {
		return ProfileInventory{}, errors.New("profile list missing")
	}
	out := ProfileInventory{ObservedAt: time.Now().UTC(), Source: "gobinet-wds-saved-profile-indices", Current: false, ProfileType: "3gpp", Indices: []int{}}
	seen := map[byte]bool{}
	pos := 1
	for i := 0; i < int(list[0]); i++ {
		if len(list)-pos < 3 {
			return ProfileInventory{}, errors.New("profile list entry truncated")
		}
		family, index, nameLength := list[pos], list[pos+1], int(list[pos+2])
		pos += 3
		if family != 0 || seen[index] || nameLength > len(list)-pos {
			return ProfileInventory{}, errors.New("invalid profile list entry")
		}
		// Names may contain personal information; skip without conversion or retention.
		pos += nameLength
		seen[index] = true
		out.Indices = append(out.Indices, int(index))
	}
	if pos != len(list) {
		return ProfileInventory{}, errors.New("profile list trailing data")
	}
	sort.Ints(out.Indices)
	out.Profile1Present = seen[1]
	out.Profile2Present = seen[2]
	return out, nil
}
