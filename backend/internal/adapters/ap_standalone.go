package adapters

import (
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/config"
	"os"
	"path/filepath"
	"time"
)

type apChannelDecision struct {
	Channel int
	Source  string
}

func apValidChannel(channel int) bool { return channel >= 1 && channel <= 13 }

// An associated 2.4GHz channel is the only observation allowed to move an
// already verified AP. Missing/transitional/disconnected STA keeps its channel.
func apChooseChannel(current, associated, lastVerified int, fallback bool) (apChannelDecision, error) {
	if apValidChannel(associated) {
		return apChannelDecision{associated, "sta"}, nil
	}
	if apValidChannel(current) {
		return apChannelDecision{current, "retained"}, nil
	}
	if !fallback {
		return apChannelDecision{}, errors.New("AP_WAITING_STA")
	}
	if apValidChannel(lastVerified) {
		return apChannelDecision{lastVerified, "last-verified"}, nil
	}
	return apChannelDecision{6, "standalone"}, nil
}

type apLastChannel struct {
	Channel int       `json:"channel"`
	BootID  string    `json:"bootId"`
	At      time.Time `json:"at"`
}

func apReadLastChannel(directory, boot string) int {
	if boot == "" {
		return 0
	}
	raw, e := os.ReadFile(filepath.Join(apPrivate(directory), "last-channel.json"))
	var value apLastChannel
	if e != nil || len(raw) > 2048 || json.Unmarshal(raw, &value) != nil || value.BootID != boot || !apValidChannel(value.Channel) {
		return 0
	}
	return value.Channel
}
func apRememberChannel(directory string, channel int) {
	boot := read("/proc/sys/kernel/random/boot_id")
	if boot == "" || !apValidChannel(channel) || apReadLastChannel(directory, boot) == channel {
		return
	}
	_ = config.Atomic(filepath.Join(apPrivate(directory), "last-channel.json"), apLastChannel{channel, boot, time.Now().UTC()})
}
