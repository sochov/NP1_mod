package adapters

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Both Linux paths are ready. Selection must follow the user's preference,
// and every owned forwarding resource must use that selected path even if
// the host's ordinary default continues to use the other connection.
func TestAPPreferredDualReadySelectionAndPlan(t *testing.T) {
	now := time.Now()
	cell := sharingTestUpstream(t, "cellular", "usb0.1", now)
	wifi := sharingTestUpstream(t, "wifi", "wlan0", now)
	cellBefore, wifiBefore := cell, wifi
	for _, preference := range []string{"cellular", "", "wifi"} {
		t.Run(preference, func(t *testing.T) {
			var calls []string
			selected, err := readAPPreferredUpstream(context.Background(), preference,
				func(context.Context) (APSharingUpstream, error) { calls = append(calls, "cellular"); return cell, nil },
				func(context.Context) (APSharingUpstream, error) { calls = append(calls, "wifi"); return wifi, nil },
				func() time.Time { return now })
			kind, expectedIF, otherIF := "cellular", "usb0.1", "wlan0"
			if preference == "wifi" {
				kind, expectedIF, otherIF = "wifi", "wlan0", "usb0.1"
			}
			if err != nil || selected.kind != kind || selected.iface != expectedIF || !reflect.DeepEqual(calls, []string{kind}) {
				t.Fatalf("preference=%q selected=%v calls=%v err=%v", preference, selected, calls, err)
			}
			plan, err := apBuildSharingPlan(strings.Repeat("a", 32), &selected)
			if err != nil {
				t.Fatal(err)
			}
			text := plan.restoreText()
			if !strings.Contains(text, "-o "+expectedIF+" -j MASQUERADE") || strings.Contains(text, otherIF) || !strings.Contains(plan.dns, "@"+expectedIF+"\n") || strings.Contains(plan.dns, otherIF) {
				t.Fatal("NAT/forwarding/DNS did not retain the selected interface")
			}
			defaultFound := false
			for _, route := range plan.routes {
				if route[0] == "default" {
					defaultFound = true
					if !strings.Contains(strings.Join(route, " "), "dev "+expectedIF+" src ") || strings.Contains(strings.Join(route, " "), otherIF) {
						t.Fatal("source-policy default uses a different upstream")
					}
				}
			}
			if !defaultFound {
				t.Fatal("selected upstream has no owned source-policy default")
			}
		})
	}
	if !reflect.DeepEqual(cell, cellBefore) || !reflect.DeepEqual(wifi, wifiBefore) {
		t.Fatal("selection/planning mutated either live connection proof")
	}
}
