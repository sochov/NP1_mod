package adapters

import "testing"

func TestDefaultCounterInterface(t *testing.T) {
	routes := "Iface Destination Gateway Flags RefCnt Use Metric Mask\n" +
		"wlan0 00000000 014DA8C0 0003 0 0 200 00000000\n" +
		"usb0 00000000 00000000 0000 0 0 0 00000000\n" +
		"usb0.1 00000000 01010101 0003 0 0 100 00000000\n" +
		"tailscale0 00000064 00000000 0001 0 0 0 000000FF\n"
	if actual := defaultInterface(routes); actual != "usb0.1" {
		t.Fatal(actual)
	}
	if actual := defaultInterface("Iface Destination Gateway Flags RefCnt Use Metric Mask\n"); actual != "" {
		t.Fatal(actual)
	}
}
