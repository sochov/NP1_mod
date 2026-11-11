package modem

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
)

const stockCMExecutable = "/usr/bin/quectel-CM"
const stockCMExecutableSHA256 = "def4cbfafdb1aa12d0ed1ff6994e7b9a11f30d33cd2e0a81e4d86189f60e9d89"
const stockCMOwnerExecutable = "/usr/local/bin/NEventWatcherCom"
const stockCMOwnerExecutableSHA256 = "3a57830dbc5e3ad26355bb14ca28141f8e82eba94b10b8b619dabea60901ec06"

// Private provenance only. Do not accidentally fmt/JSON cmdline, PID or groups
// into a Web status. The current concurrent-QMI rejection remains unchanged.
type stockCMProcess struct {
	pid                                         uint32
	start, executable, digest, networkNamespace string
	uids, gids                                  [4]uint32
	cgroups                                     map[string]string
	arguments                                   []string
}

func (stockCMProcess) String() string     { return "[private stock CM process identity]" }
func (v stockCMProcess) GoString() string { return v.String() }
func (stockCMProcess) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Private bool `json:"private"`
	}{true})
}

func stockCMProcessStart(raw []byte, pid uint32) (string, error) {
	if pid == 0 || len(raw) > 8192 || !bytes.HasPrefix(raw, []byte(strconv.FormatUint(uint64(pid), 10)+" (")) {
		return "", errors.New("STOCK_CM_PROCESS_STAT_INVALID")
	}
	end := bytes.LastIndex(raw, []byte(") "))
	if end < 0 {
		return "", errors.New("STOCK_CM_PROCESS_STAT_INVALID")
	}
	fields := strings.Fields(string(raw[end+2:]))
	if len(fields) < 20 || len(fields[0]) != 1 || !strings.Contains("RSDTtIW", fields[0]) {
		return "", errors.New("STOCK_CM_PROCESS_STAT_INVALID")
	}
	start, e := strconv.ParseUint(fields[19], 10, 64)
	if e != nil || start == 0 {
		return "", errors.New("STOCK_CM_PROCESS_STAT_INVALID")
	}
	return fields[19], nil
}

func stockCMProcessIDs(raw []byte) (uids, gids [4]uint32, err error) {
	if len(raw) > 32768 {
		return uids, gids, errors.New("STOCK_CM_PROCESS_IDS_INVALID")
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		for _, key := range []string{"Uid:", "Gid:"} {
			if !strings.HasPrefix(line, key) {
				continue
			}
			if seen[key] {
				return uids, gids, errors.New("STOCK_CM_PROCESS_IDS_INVALID")
			}
			seen[key] = true
			fields := strings.Fields(strings.TrimPrefix(line, key))
			if len(fields) != 4 {
				return uids, gids, errors.New("STOCK_CM_PROCESS_IDS_INVALID")
			}
			for i, field := range fields {
				v, e := strconv.ParseUint(field, 10, 32)
				if e != nil {
					return uids, gids, errors.New("STOCK_CM_PROCESS_IDS_INVALID")
				}
				if key == "Uid:" {
					uids[i] = uint32(v)
				} else {
					gids[i] = uint32(v)
				}
			}
		}
	}
	if len(seen) != 2 {
		return uids, gids, errors.New("STOCK_CM_PROCESS_IDS_INVALID")
	}
	return
}

func stockCMProcessCgroups(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 16384 {
		return nil, errors.New("STOCK_CM_CGROUP_INVALID")
	}
	groups := map[string]string{}
	unit := false
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || len(groups) >= 64 {
			return nil, errors.New("STOCK_CM_CGROUP_INVALID")
		}
		if _, e := strconv.ParseUint(parts[0], 10, 32); e != nil {
			return nil, errors.New("STOCK_CM_CGROUP_INVALID")
		}
		for _, c := range parts[1] {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '=' || c == ',') {
				return nil, errors.New("STOCK_CM_CGROUP_INVALID")
			}
		}
		path := parts[2]
		if !strings.HasPrefix(path, "/") || strings.Contains(path, "//") || strings.Contains(path, "/../") || strings.HasSuffix(path, "/..") {
			return nil, errors.New("STOCK_CM_CGROUP_INVALID")
		}
		for _, c := range path {
			if c <= 32 || c > 126 || c == ':' {
				return nil, errors.New("STOCK_CM_CGROUP_INVALID")
			}
		}
		key := parts[0] + ":" + parts[1]
		if _, ok := groups[key]; ok {
			return nil, errors.New("STOCK_CM_CGROUP_INVALID")
		}
		groups[key] = path
		unit = unit || strings.HasSuffix(path, "/NEventWatcherCom.service")
	}
	if !unit {
		return nil, errors.New("STOCK_CM_CGROUP_OWNER_UNVERIFIED")
	}
	return groups, nil
}

func stockCMProcessArguments(raw []byte) ([]string, error) {
	if len(raw) < 2 || len(raw) > 256 || raw[len(raw)-1] != 0 {
		return nil, errors.New("STOCK_CM_ARGUMENTS_UNVERIFIED")
	}
	words := strings.Split(string(raw[:len(raw)-1]), "\x00")
	// Pinned liblteapi starts profile1 as the fixed `quectel-CM -n 1&`.
	// No credentials, arbitrary profile/index, kill/reset or configuration args.
	if len(words) != 3 || words[0] != "quectel-CM" && words[0] != stockCMExecutable || words[1] != "-n" || words[2] != "1" {
		return nil, errors.New("STOCK_CM_ARGUMENTS_UNVERIFIED")
	}
	return words, nil
}

func verifiedStockCMOwner(owner stockCMProcess) error {
	ownerStart, ownerStartError := strconv.ParseUint(owner.start, 10, 64)
	namespace := strings.TrimSuffix(strings.TrimPrefix(owner.networkNamespace, "net:["), "]")
	namespaceID, namespaceError := strconv.ParseUint(namespace, 10, 64)
	if owner.pid == 0 || ownerStartError != nil || ownerStart == 0 || owner.executable != stockCMOwnerExecutable || owner.digest != stockCMOwnerExecutableSHA256 || owner.uids[0] == 0 || namespaceError != nil || namespaceID == 0 || owner.networkNamespace != "net:["+namespace+"]" {
		return errors.New("STOCK_CM_OWNER_UNVERIFIED")
	}
	var groupLines []string
	for key, path := range owner.cgroups {
		groupLines = append(groupLines, key+":"+path)
	}
	if _, e := stockCMProcessCgroups([]byte(strings.Join(groupLines, "\n"))); e != nil {
		return e
	}
	for i := 0; i < 4; i++ {
		if owner.uids[i] != owner.uids[0] || owner.gids[i] != owner.gids[0] {
			return errors.New("STOCK_CM_OWNER_CREDENTIALS_UNVERIFIED")
		}
	}
	return nil
}

func verifiedStockCMProcess(candidate, owner stockCMProcess) error {
	if e := verifiedStockCMOwner(owner); e != nil {
		return e
	}
	cmStart, cmStartError := strconv.ParseUint(candidate.start, 10, 64)
	if candidate.pid == 0 || candidate.pid == owner.pid || cmStartError != nil || cmStart == 0 || candidate.executable != stockCMExecutable || candidate.digest != stockCMExecutableSHA256 || candidate.uids != owner.uids || candidate.gids != owner.gids || !reflect.DeepEqual(candidate.cgroups, owner.cgroups) || candidate.networkNamespace != owner.networkNamespace {
		return errors.New("STOCK_CM_IDENTITY_UNVERIFIED")
	}
	if _, e := stockCMProcessArguments([]byte(strings.Join(candidate.arguments, "\x00") + "\x00")); e != nil {
		return e
	}
	return nil
}

func stockCMProcessUnchanged(before, after stockCMProcess) bool {
	return before.pid == after.pid && before.start == after.start && before.executable == after.executable && before.digest == after.digest && before.uids == after.uids && before.gids == after.gids && before.networkNamespace == after.networkNamespace && reflect.DeepEqual(before.arguments, after.arguments) && reflect.DeepEqual(before.cgroups, after.cgroups)
}
