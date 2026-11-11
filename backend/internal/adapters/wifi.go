package adapters

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"os/user"
	"strconv"
	"time"
)

// VendorStatus issues only STATUS to the vendor-owned supplicant; no scan/configuration or second daemon.
// Bind the reply socket in our writable runtime directory, not the read-only /tmp sandbox.
func VendorStatus() (string, error) {
	b := make([]byte, 8)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	local := "/run/np1-control/wpa-" + hex.EncodeToString(b)
	c, e := net.DialUnix("unixgram", &net.UnixAddr{Name: local, Net: "unixgram"}, &net.UnixAddr{Name: "/run/wifi/wpa_supplicant/wlan0", Net: "unixgram"})
	if e != nil {
		return "", e
	}
	defer c.Close()
	defer os.Remove(local)
	group, e := user.LookupGroup("NEventWatcherCom")
	if e != nil {
		return "", e
	}
	gid, e := strconv.Atoi(group.Gid)
	if e != nil {
		return "", e
	}
	if e = os.Chown(local, 0, gid); e != nil {
		return "", e
	}
	if e = os.Chmod(local, 0660); e != nil {
		return "", e
	}
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, e = c.Write([]byte("STATUS")); e != nil {
		return "", e
	}
	data := make([]byte, 8192)
	n, e := c.Read(data)
	return string(data[:n]), e
}
