package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/np1-control-center/ncc/internal/config"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (c *WiFiController) readPrivate(name string, out any) error {
	directory, e := os.Lstat(c.directory)
	if e != nil {
		return e
	}
	if !directory.IsDir() || directory.Mode()&os.ModeSymlink != 0 {
		return errors.New("WIFI_PRIVATE_CONFIG_INVALID")
	}
	path := filepath.Join(c.directory, name)
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() > 32768 || (runtime.GOOS == "linux" && st.Mode().Perm()&0077 != 0) {
		return errors.New("WIFI_PRIVATE_CONFIG_INVALID")
	}
	f, e := os.Open(path)
	if e != nil {
		return errors.New("WIFI_PRIVATE_CONFIG_UNAVAILABLE")
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 32769))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return errors.New("WIFI_PRIVATE_CONFIG_INVALID")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("WIFI_PRIVATE_CONFIG_INVALID")
	}
	return nil
}
func (c *WiFiController) writePrivate(name string, out any) error {
	if e := os.MkdirAll(c.directory, 0700); e != nil {
		return errors.New("WIFI_PRIVATE_CONFIG_UNAVAILABLE")
	}
	st, e := os.Lstat(c.directory)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("WIFI_PRIVATE_CONFIG_INVALID")
	}
	if e = os.Chmod(c.directory, 0700); e != nil {
		return errors.New("WIFI_PRIVATE_CONFIG_UNAVAILABLE")
	}
	if e = config.Atomic(filepath.Join(c.directory, name), out); e != nil {
		return errors.New("WIFI_PRIVATE_CONFIG_WRITE_FAILED")
	}
	return nil
}
func validWiFiProfile(p wifiProfile) bool {
	return strings.HasPrefix(p.ID, "ncc-") && len(p.ID) == 40 && wifiSSID(p.SSID) && (p.Security == "open" && p.Password == "" || p.Security == "wpa2" && wifiPassword(p.Password))
}
func (c *WiFiController) loadProfiles() ([]wifiProfile, error) {
	profiles := []wifiProfile{}
	e := c.readPrivate("profiles.json", &profiles)
	if os.IsNotExist(e) {
		return profiles, nil
	}
	if e != nil {
		return nil, e
	}
	if len(profiles) > 32 {
		return nil, errors.New("WIFI_SAVED_PROFILES_INVALID")
	}
	seen := map[string]bool{}
	for _, p := range profiles {
		if !validWiFiProfile(p) || seen[p.ID] {
			return nil, errors.New("WIFI_SAVED_PROFILES_INVALID")
		}
		seen[p.ID] = true
	}
	return profiles, nil
}
func (c *WiFiController) storeProfile(profile wifiProfile) error {
	profiles, e := c.loadProfiles()
	if e != nil {
		return e
	}
	for i, p := range profiles {
		if p.ID == profile.ID {
			profiles[i] = profile
			return c.writePrivate("profiles.json", profiles)
		}
	}
	if len(profiles) >= 32 {
		return errors.New("WIFI_SAVED_PROFILE_LIMIT")
	}
	profiles = append(profiles, profile)
	return c.writePrivate("profiles.json", profiles)
}
func (c *WiFiController) savePending(pending *wifiPending) error {
	return c.writePrivate("pending.json", pending)
}
func (c *WiFiController) clearPending() error {
	e := os.Remove(filepath.Join(c.directory, "pending.json"))
	if e != nil && !os.IsNotExist(e) {
		return errors.New("WIFI_JOURNAL_CLEAR_FAILED")
	}
	return nil
}
func (c *WiFiController) recoverIfNeeded(_ context.Context) error {
	if c.pending != nil {
		if !c.pending.Public.ExpiresAt.After(time.Now()) {
			_, e := c.rollbackLocked("confirmation-timeout")
			return e
		}
		return nil
	}
	var p wifiPending
	e := c.readPrivate("pending.json", &p)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if len(p.Public.ID) != 36 || p.Public.State != "pending" || p.TargetID < 0 || p.TargetID > 1000000 || p.OldID != nil && (*p.OldID < 0 || *p.OldID > 1000000) || p.CreatedID != nil && (*p.CreatedID != p.TargetID || p.Profile == nil || !validWiFiProfile(*p.Profile)) {
		return errors.New("WIFI_JOURNAL_INVALID")
	}
	c.pending = &p
	_, e = c.rollbackLocked("service-restarted")
	return e
}
func (c *WiFiController) Recover(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recoverIfNeeded(ctx)
}
