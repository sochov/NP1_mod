package adapters

import "testing"

func TestDriverBytePresenceDistinguishesUnsupportedFromMeasuredZero(t *testing.T) {
	clients, _ := apParseStations("00:11:22:33:44:55\nflags=[ASSOC]\nrx_bytes=0\ntx_bytes=0\n")
	packetsOnly := "Station 00:11:22:33:44:55 (on nccap0)\n\trx packets: 40\n\ttx packets: 20\n"
	apApplyDriverTraffic(clients, packetsOnly)
	if clients[0].ReceivedBytes != nil || clients[0].SentBytes != nil {
		t.Fatal("unsupported driver bytes became measured zero")
	}
	apApplyDriverTraffic(clients, packetsOnly+"\trx bytes: 0\n\ttx bytes: 18446744073709551615\n")
	if clients[0].SentBytes == nil || *clients[0].SentBytes != "0" || clients[0].ReceivedBytes == nil || *clients[0].ReceivedBytes != "18446744073709551615" {
		t.Fatal(clients)
	}
	apApplyDriverTraffic(clients, packetsOnly+"\trx bytes: 15\n\trx bytes: 16\n")
	if clients[0].SentBytes != nil {
		t.Fatal("duplicate attribute accepted")
	}
	apApplyDriverTraffic(clients, "Station 00:11:22:33:44:55 (on wlan0)\n\trx bytes: 10\n")
	if clients[0].SentBytes != nil {
		t.Fatal("foreign VIF accepted")
	}
}
