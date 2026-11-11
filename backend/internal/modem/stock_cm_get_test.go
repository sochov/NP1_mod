package modem

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestStockCMDescriptorExceptionIsOnlyExactOwnFDAndPinnedCM(t *testing.T) {
	base := []stockCMDescriptor{{10, 4}, {10, 3}, {20, 8}}
	got, err := stockCMDescriptorSet(10, 20, 8, base)
	if err != nil || !reflect.DeepEqual(got, []int{3, 4}) {
		t.Fatal("fixed FD exception failed", err)
	}
	for _, rows := range [][]stockCMDescriptor{
		{{10, 3}, {20, 8}, {20, 9}}, // same process but a different QMI FD
		{{10, 3}, {30, 8}},          // the same FD number in another process
		{{10, 3}},                   // the claimed own FD was never observed
		{{20, 8}},                   // the stock CM disappeared
		{{10, 3}, {10, 3}, {20, 8}}, // duplicate observation
	} {
		if _, err := stockCMDescriptorSet(10, 20, 8, rows); err == nil {
			t.Fatal("foreign, absent or ambiguous descriptors accepted")
		}
	}
	if _, err := stockCMDescriptorSet(10, 20, -1, base[:2]); err != nil {
		t.Fatal("closed own FD not accepted", err)
	}
	if _, err := stockCMDescriptorSet(10, 20, -1, base); err == nil {
		t.Fatal("post-close FD leak ignored")
	}
}

func TestStockCMFixedGetsDoNotPermitDataCallOrProfileWrites(t *testing.T) {
	for _, v := range [][3]uint32{{3, 1, 0x4f}, {3, 2, 0x24}, {3, 3, 0x31}, {1, 5, 0x2b}, {1, 6, 0x2b}, {1, 7, 0x2a}} {
		if !stockCMFixedGetAllowed(v[0], uint16(v[1]), uint16(v[2])) {
			t.Fatal("fixed GET rejected")
		}
	}
	for _, v := range [][3]uint32{{1, 1, 0x20}, {1, 1, 0x21}, {1, 5, 0x27}, {1, 5, 0x28}, {1, 5, 0x4d}, {0, 1, 0x22}, {26, 1, 0x20}, {1, 4, 0x2d}, {1, 6, 0x2a}, {3, 2, 0x4f}} {
		if stockCMFixedGetAllowed(v[0], uint16(v[1]), uint16(v[2])) {
			t.Fatal("nonfixed request accepted")
		}
	}
}

func TestStockCMQueryGuardRejectsBeforeSendAndDiscardsAfterChange(t *testing.T) {
	want := errors.New("STOCK_CM_GET_PROVENANCE_CHANGED")
	for _, failAt := range []int{1, 2} {
		checks, sends := 0, 0
		guard := stockCMGuardedExchange{service: 3, verify: func() error {
			checks++
			if checks == failAt {
				return want
			}
			return nil
		}, exchange: func(context.Context, uint16, uint16) ([]byte, error) {
			sends++
			return []byte("private raw response"), nil
		}}
		frame, err := guard.run(context.Background(), 1, 0x4f)
		if !errors.Is(err, want) || frame != nil || sends != failAt-1 {
			t.Fatal("request/result escaped changed owner")
		}
		_, _ = guard.run(context.Background(), 2, 0x24)
		if checks != failAt || sends != failAt-1 {
			t.Fatal("queries continued after provenance failure")
		}
	}
	checks, sends := 0, 0
	guard := stockCMGuardedExchange{service: 1, verify: func() error { checks++; return nil }, exchange: func(context.Context, uint16, uint16) ([]byte, error) { sends++; return nil, nil }}
	if _, err := guard.run(context.Background(), 5, 0x27); err == nil || checks != 0 || sends != 0 {
		t.Fatal("write reached transport")
	}
}

func TestStockCMLeaseRejectsProcessOrDescriptorChangeAndRedacts(t *testing.T) {
	cm, owner := stockCMFixture()
	before := stockCMLease{owner: owner, cm: cm, descriptors: []int{3, 4}}
	if !stockCMLeaseMatches(before, before) {
		t.Fatal("unchanged snapshot rejected")
	}
	after := before
	after.descriptors = []int{3, 5}
	if stockCMLeaseMatches(before, after) {
		t.Fatal("CM descriptor replacement accepted")
	}
	after = before
	after.cm.start = "changed"
	if stockCMLeaseMatches(before, after) {
		t.Fatal("CM PID reuse accepted")
	}
	after = before
	after.owner.start = "changed"
	if stockCMLeaseMatches(before, after) {
		t.Fatal("owner restart accepted")
	}
	if out := fmt.Sprintf("%+v %#v", before, before); strings.Contains(out, "quectel-CM") || strings.Contains(out, "456") {
		t.Fatal("private lease exposed")
	}
}
