package adapters

import (
	"strings"
	"testing"
)

func TestNativeSettingOwnerRequiresActiveStableProcess(t *testing.T) {
	valid := "MainPID=1234\nInvocationID=" + strings.Repeat("a", 32) + "\nActiveState=active\nSubState=running\nJob=\n"
	owner, err := parseNativeSettingOwner(valid)
	if err != nil || owner.pid != "1234" {
		t.Fatal("valid owner rejected", err)
	}
	for _, text := range []string{strings.Replace(valid, "MainPID=1234", "MainPID=0", 1), strings.Replace(valid, "MainPID=1234", "MainPID=01", 1), strings.Replace(valid, "ActiveState=active", "ActiveState=inactive", 1), strings.Replace(valid, "ActiveState=active", "ActiveState=failed", 1), strings.Replace(valid, "SubState=running", "SubState=stop-sigterm", 1), strings.Replace(valid, "Job=", "Job=42 /job/42", 1), strings.Replace(valid, "Job=\n", "", 1), strings.Replace(valid, strings.Repeat("a", 32), "unknown", 1), valid + "MainPID=1234\n"} {
		if _, err := parseNativeSettingOwner(text); err == nil {
			t.Fatal("stopped/unknown/transitional owner accepted")
		}
	}
}
