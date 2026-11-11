package adapters

import (
	"errors"
	"strconv"
	"strings"
)

type nativeSettingIdentity struct{ pid, invocation string }

func parseNativeSettingOwner(text string) (nativeSettingIdentity, error) {
	fields := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			if _, duplicate := fields[key]; duplicate {
				return nativeSettingIdentity{}, errors.New("duplicate Setting state")
			}
			fields[key] = value
		}
	}
	pid, err := strconv.ParseInt(fields["MainPID"], 10, 32)
	job, hasJob := fields["Job"]
	if err != nil || pid <= 0 || strconv.FormatInt(pid, 10) != fields["MainPID"] || fields["ActiveState"] != "active" || fields["SubState"] != "running" || !hasJob || job != "" || !stockInvocationPattern.MatchString(fields["InvocationID"]) {
		return nativeSettingIdentity{}, errors.New("Setting owner is not active and stable")
	}
	return nativeSettingIdentity{strconv.FormatInt(pid, 10), fields["InvocationID"]}, nil
}

func nativeSettingOwnerState() (nativeSettingIdentity, error) {
	text, err := command("systemctl", "show", "app-@Setting.service", "-p", "MainPID", "-p", "InvocationID", "-p", "ActiveState", "-p", "SubState", "-p", "Job")
	if err != nil {
		return nativeSettingIdentity{}, err
	}
	return parseNativeSettingOwner(text)
}
