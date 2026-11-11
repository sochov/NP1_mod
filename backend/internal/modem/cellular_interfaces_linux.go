//go:build linux

package modem

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// VerifyCellularInterfaces returns only fixed interfaces proven to belong to
// the pinned EC25/GobiNet USB instance. It performs no AT/QMI operation and
// does not require quectel-CM to remain running. IP/route/link freshness must be
// checked separately immediately before a forwarding policy is selected.
func VerifyCellularInterfaces() ([]string, error) {
	if e := verifyGobiNet(); e != nil {
		return nil, errors.New("CELLULAR_GOBINET_UNVERIFIED")
	}
	physical, e := filepath.EvalSymlinks("/sys/class/net/usb0/device")
	if e != nil {
		return nil, errors.New("CELLULAR_PHYSICAL_INTERFACE_UNVERIFIED")
	}
	driver, e := filepath.EvalSymlinks(filepath.Join(physical, "driver"))
	if e != nil || filepath.Base(driver) != "GobiNet" {
		return nil, errors.New("CELLULAR_PHYSICAL_DRIVER_UNVERIFIED")
	}
	matched := false
	for current := physical; current != "/"; current = filepath.Dir(current) {
		vendor, readError := os.ReadFile(filepath.Join(current, "idVendor"))
		if readError != nil {
			continue
		}
		product, readError := os.ReadFile(filepath.Join(current, "idProduct"))
		matched = readError == nil && strings.TrimSpace(string(vendor)) == "2c7c" && strings.TrimSpace(string(product)) == "0125"
		break
	}
	if !matched {
		return nil, errors.New("CELLULAR_USB_OWNER_UNVERIFIED")
	}
	parent, e := filepath.EvalSymlinks("/sys/class/net/usb0")
	if e != nil || parent != filepath.Join(physical, "net", "usb0") {
		return nil, errors.New("CELLULAR_PARENT_NETDEV_UNVERIFIED")
	}
	names := []string{"usb0"}
	for _, name := range []string{"usb0.1", "usb0.2"} {
		if _, e := os.Lstat("/sys/class/net/" + name); errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return nil, errors.New("CELLULAR_CHILD_INTERFACE_UNAVAILABLE")
		}
		device, e := filepath.EvalSymlinks("/sys/class/net/" + name + "/device")
		if e != nil || device != parent {
			return nil, errors.New("CELLULAR_CHILD_PARENT_UNVERIFIED")
		}
		names = append(names, name)
	}
	return names, nil
}
