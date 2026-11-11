package modem

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func stockCMFixture() (stockCMProcess, stockCMProcess) {
	owner := stockCMProcess{pid: 123, start: "100", executable: stockCMOwnerExecutable, digest: stockCMOwnerExecutableSHA256, uids: [4]uint32{100, 100, 100, 100}, gids: [4]uint32{100, 100, 100, 100}, cgroups: map[string]string{"1:name=systemd": "/system.slice/NEventWatcherCom.service"}, networkNamespace: "net:[100]"}
	cm := owner
	cm.pid = 456
	cm.start = "200"
	cm.executable = stockCMExecutable
	cm.digest = stockCMExecutableSHA256
	cm.arguments = []string{"quectel-CM", "-n", "1"}
	return cm, owner
}

func TestStockCMProofRequiresFirmwareOwnerCredentialsNamespaceAndFixedProfile(t *testing.T) {
	cm, owner := stockCMFixture()
	if e := verifiedStockCMProcess(cm, owner); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		change func(*stockCMProcess)
	}{
		{"different exe", func(v *stockCMProcess) { v.executable = "/tmp/quectel-CM" }},
		{"different hash", func(v *stockCMProcess) { v.digest = strings.Repeat("0", 64) }},
		{"different uid", func(v *stockCMProcess) { v.uids[1] = 0 }},
		{"different gid", func(v *stockCMProcess) { v.gids[0] = 0 }},
		{"different cgroup", func(v *stockCMProcess) {
			v.cgroups = map[string]string{"1:name=systemd": "/system.slice/foreign.service"}
		}},
		{"different netns", func(v *stockCMProcess) { v.networkNamespace = "net:[200]" }},
		{"profile2", func(v *stockCMProcess) { v.arguments = []string{"quectel-CM", "-n", "2"} }},
		{"credential argv", func(v *stockCMProcess) { v.arguments = []string{"quectel-CM", "-s", "private-value", "-n", "1"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm, owner := stockCMFixture()
			tc.change(&cm)
			if e := verifiedStockCMProcess(cm, owner); e == nil || strings.Contains(e.Error(), "private-value") {
				t.Fatal("unsafe identity or leaked detail")
			}
		})
	}
	_, bad := stockCMFixture()
	bad.uids = [4]uint32{}
	if verifiedStockCMProcess(cm, bad) == nil {
		t.Fatal("root-launched owner substituted")
	}
}

func TestStockCMOwnerSnapshotCannotBeReusedAfterPIDOrCredentialChange(t *testing.T) {
	before, _ := stockCMFixture()
	after := before
	if !stockCMProcessUnchanged(before, after) {
		t.Fatal("unchanged identity rejected")
	}
	after.start = "201"
	if stockCMProcessUnchanged(before, after) {
		t.Fatal("PID reuse accepted")
	}
	text := fmt.Sprintf("%+v %#v", before, before)
	raw, _ := json.Marshal(before)
	for _, out := range []string{text, string(raw)} {
		if strings.Contains(out, "quectel-CM") || strings.Contains(out, "-n") || strings.Contains(out, "456") {
			t.Fatal("private metadata exposed")
		}
	}
}

func TestStockCMProcMetadataBoundsAndMalformedValues(t *testing.T) {
	fields := []string{"S"}
	for i := 1; i < 19; i++ {
		fields = append(fields, "0")
	}
	fields = append(fields, "200")
	valid := []byte("456 (quectel (CM)) " + strings.Join(fields, " "))
	if start, e := stockCMProcessStart(valid, 456); e != nil || start != "200" {
		t.Fatal(e)
	}
	for _, raw := range [][]byte{[]byte("456 broken"), []byte("457 (x) " + strings.Join(fields, " ")), []byte(strings.Repeat("x", 8193))} {
		if _, e := stockCMProcessStart(raw, 456); e == nil {
			t.Fatal("bad stat accepted")
		}
	}
	for _, raw := range []string{"Uid:\t100 100 100 100\nGid:\t100 100 100 100\n", "Uid:\t100 100 100\nGid:\t100 100 100 100", "Uid:\t100 100 100 100\nUid:\t100 100 100 100\nGid:\t100 100 100 100"} {
		_, _, e := stockCMProcessIDs([]byte(raw))
		if strings.Count(raw, "Uid:") == 1 && strings.Contains(raw, "Uid:\t100 100 100 100\n") {
			if e != nil {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal("bad IDs accepted")
		}
	}
	if _, e := stockCMProcessCgroups([]byte("1:name=systemd:/system.slice/NEventWatcherCom.service\n2:cpu:/\n")); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{"1:name=systemd:/system.slice/foreign.service", "1:name=systemd:/system.slice/NEventWatcherCom.service\n1:name=systemd:/", "1:name=systemd:/../NEventWatcherCom.service", "1:name=systemd:/system.slice/NEventWatcherCom.service/subgroup"} {
		if _, e := stockCMProcessCgroups([]byte(raw)); e == nil {
			t.Fatal("unsafe cgroup accepted")
		}
	}
	for _, raw := range []string{"quectel-CM\x00-n\x001", "quectel-CM\x00-n\x002\x00", "quectel-CM\x00-n\x001\x00-k\x001\x00", "quectel-CM\x00-n\x001\x00\x00"} {
		if _, e := stockCMProcessArguments([]byte(raw)); e == nil {
			t.Fatal("unsafe argv accepted")
		}
	}
}
