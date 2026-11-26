package androidauto

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/androidauto/aap"
	"github.com/np1-control-center/ncc/internal/androidauto/stockbt"
	"github.com/np1-control-center/ncc/internal/config"
)

// Wireless Android Auto over the stock Bluetooth stack. The head unit pairs
// with NP1 (the owner opens a pairing window and picks NP1 on the head unit),
// then NP1 connects the head unit's wireless projection RFCOMM service, learns
// its WiFi network over the WiFi Projection Protocol, joins it and opens the
// projection session over TCP.
//
// The stock stack keeps the RFCOMM UUIDs of its vendor SPP slots in memory;
// VSSPP3 is unused by the stock apps and gets the projection UUID while BT
// runs (stockbt.SetVendorSlot, nothing is written to disk). Link keys of
// paired head units are kept here because the stack forgets them on reboot.

const (
	btSlot         = 3 // VSSPP3
	btProfile      = stockbt.ProfileVSSPP3
	btLocalUUID    = "a3b1c4d0-6e2f-4b7a-9c1d-2f5e8a7b0c13"
	btPairWindow   = 2 * time.Minute
	btRetry        = 20 * time.Second
	btRetryNoJoin  = 2 * time.Minute
	btConnectLimit = 30 * time.Second
	btHandshake    = 45 * time.Second
	btStoreFile    = "bluetooth.json"
)

// ErrNoWiFiJoin is returned while joining a head unit's WiFi is not available.
var ErrNoWiFiJoin = errors.New("ナビのWi-Fiへの接続にはまだ対応していません")

// BTHeadUnit is a paired head unit.
type BTHeadUnit struct {
	Address  string     `json:"address"`
	PairedAt time.Time  `json:"pairedAt"`
	LastSeen *time.Time `json:"lastSeen,omitempty"`
}

// BTStatus is the Bluetooth part of the status.
type BTStatus struct {
	State        string       `json:"state"` // off, unavailable, idle, pairing, connecting, handshake, joining, projecting
	Reason       string       `json:"reason,omitempty"`
	PairingUntil *time.Time   `json:"pairingUntil,omitempty"`
	HeadUnits    []BTHeadUnit `json:"headUnits"`
	Peer         string       `json:"peer,omitempty"`
	Network      string       `json:"network,omitempty"` // SSID the head unit offered
	LastError    string       `json:"lastError,omitempty"`
	LastErrorAt  *time.Time   `json:"lastErrorAt,omitempty"`
}

type btStored struct {
	Address  string     `json:"address"`
	Key      string     `json:"key"`
	KeyType  int        `json:"keyType"`
	PairedAt time.Time  `json:"pairedAt"`
	LastSeen *time.Time `json:"lastSeen,omitempty"`
}

type btCommand struct {
	pair   *bool
	remove string
	done   chan error
}

type btManager struct {
	s     *Service
	path  string
	cmds  chan btCommand
	mu    sync.Mutex
	store []btStored
	st    BTStatus
	next  int // round-robin position among paired head units
}

func newBTManager(s *Service) *btManager {
	m := &btManager{s: s, path: filepath.Join(s.cfg.DataDir, btStoreFile), cmds: make(chan btCommand)}
	m.st = BTStatus{State: "off", HeadUnits: []BTHeadUnit{}}
	m.load()
	return m
}

func (m *btManager) load() {
	b, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var v struct {
		HeadUnits []btStored `json:"headUnits"`
	}
	if json.Unmarshal(b, &v) == nil {
		m.store = v.HeadUnits
	}
	m.publishLocked()
}

func (m *btManager) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	return config.Atomic(m.path, struct {
		HeadUnits []btStored `json:"headUnits"`
	}{m.store})
}

func (m *btManager) publishLocked() {
	hus := make([]BTHeadUnit, 0, len(m.store))
	for _, h := range m.store {
		hus = append(hus, BTHeadUnit{Address: h.Address, PairedAt: h.PairedAt, LastSeen: h.LastSeen})
	}
	m.st.HeadUnits = hus
}

func (m *btManager) status() BTStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.st
	st.HeadUnits = slices.Clone(m.st.HeadUnits)
	return st
}

func (m *btManager) set(state, reason string) {
	m.mu.Lock()
	m.st.State, m.st.Reason = state, reason
	m.mu.Unlock()
}

func (m *btManager) fail(err error) {
	m.mu.Lock()
	now := time.Now()
	m.st.LastError, m.st.LastErrorAt = err.Error(), &now
	m.mu.Unlock()
	m.s.cfg.Logf("android auto bluetooth: %v", err)
}

// command hands a request to the running manager loop.
func (m *btManager) command(ctx context.Context, c btCommand) error {
	c.done = make(chan error, 1)
	select {
	case m.cmds <- c:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return errors.New("Bluetoothの処理が応答しません")
	}
	select {
	case err := <-c.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *btManager) remember(a stockbt.Addr, k stockbt.LinkKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	rec := btStored{Address: a.String(), Key: hex.EncodeToString(k.Key[:]), KeyType: int(k.Type), PairedAt: now}
	if i := slices.IndexFunc(m.store, func(h btStored) bool { return h.Address == rec.Address }); i >= 0 {
		rec.PairedAt, rec.LastSeen = m.store[i].PairedAt, m.store[i].LastSeen
		m.store[i] = rec
	} else {
		m.store = append(m.store, rec)
	}
	m.publishLocked()
	return m.saveLocked()
}

func (m *btManager) known(a stockbt.Addr) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.store, func(h btStored) bool { return h.Address == a.String() })
}

func (m *btManager) seen(a stockbt.Addr) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for i := range m.store {
		if m.store[i].Address == a.String() {
			m.store[i].LastSeen = &now
		}
	}
	m.publishLocked()
	_ = m.saveLocked()
}

func parseStored(h btStored) (stockbt.Addr, [16]byte, bool) {
	a, err := stockbt.ParseAddr(h.Address)
	k, err2 := hex.DecodeString(h.Key)
	var key [16]byte
	if err != nil || err2 != nil || len(k) != 16 {
		return a, key, false
	}
	copy(key[:], k)
	return a, key, true
}

// learnKey reads the key the stack just made for a from the HCI log.
func learnKey(ctx context.Context, a stockbt.Addr) (stockbt.LinkKey, error) {
	var err error
	for range 10 {
		var k stockbt.LinkKey
		if k, err = stockbt.FindLinkKey(stockbt.SnoopLog, a); err == nil {
			return k, nil
		}
		select {
		case <-ctx.Done():
			return stockbt.LinkKey{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return stockbt.LinkKey{}, err
}

// run keeps the stock stack prepared while Bluetooth projection is wanted.
func (m *btManager) run(ctx context.Context) {
	for ctx.Err() == nil {
		if !m.wanted() {
			m.set("off", "")
			if !m.idle(ctx) {
				return
			}
			continue
		}
		c, err := stockbt.Dial(ctx)
		if err != nil {
			m.set("unavailable", "純正Bluetoothに接続できません")
			m.fail(err)
			if !m.idleFor(ctx, 10*time.Second) {
				return
			}
			continue
		}
		err = m.session(ctx, c)
		c.Close()
		if err != nil && ctx.Err() == nil {
			m.set("unavailable", "純正Bluetoothの準備に失敗しました")
			m.fail(err)
			if !m.idleFor(ctx, 10*time.Second) {
				return
			}
		}
	}
}

func (m *btManager) wanted() bool {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	return m.s.settings.Bluetooth
}

// idle waits for a settings change while off, answering commands.
func (m *btManager) idle(ctx context.Context) bool {
	return m.idleFor(ctx, 2*time.Second)
}

func (m *btManager) idleFor(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		case c := <-m.cmds:
			if c.remove != "" {
				c.done <- m.removeStored(c.remove)
			} else {
				c.done <- errors.New("Bluetooth接続が無効です")
			}
		}
	}
}

func (m *btManager) removeStored(addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.store, func(h btStored) bool { return h.Address == addr })
	if i < 0 {
		return errors.New("登録されていないナビです")
	}
	m.store = slices.Delete(m.store, i, i+1)
	m.publishLocked()
	return m.saveLocked()
}

// session prepares the stack once and then serves pairing and connections
// until the stock process goes away or Bluetooth is switched off.
func (m *btManager) session(ctx context.Context, c *stockbt.Client) error {
	pid := c.PID()
	if _, err := stockbt.SetVendorSlot(pid, btSlot, btLocalUUID, aap.WirelessUUID); err != nil {
		return err
	}
	m.mu.Lock()
	stored := slices.Clone(m.store)
	m.mu.Unlock()
	for _, h := range stored {
		if a, k, ok := parseStored(h); ok {
			if err := c.SetLinkkey(ctx, a, k); err != nil {
				m.fail(fmt.Errorf("%s のリンクキーを設定できません: %w", h.Address, err))
			}
		}
	}
	events, stop := c.Subscribe(256)
	defer stop()
	var pairUntil time.Time
	endPairing := func() {
		if !pairUntil.IsZero() {
			pairUntil = time.Time{}
			if err := c.Waiting(ctx, 0); err != nil { // back to the stock connectable, not discoverable
				m.fail(err)
			}
			m.mu.Lock()
			m.st.PairingUntil = nil
			m.mu.Unlock()
		}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if !pairUntil.IsZero() {
			_ = c.Waiting(cleanup, 0)
		}
	}()
	var attempt *btAttempt
	nextTry := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	m.set("idle", "")
	for {
		select {
		case <-ctx.Done():
			return nil
		case cmd := <-m.cmds:
			switch {
			case cmd.remove != "":
				err := m.removeStored(cmd.remove)
				if a, perr := stockbt.ParseAddr(cmd.remove); err == nil && perr == nil {
					_ = c.ClearLinkkey(ctx, a)
				}
				cmd.done <- err
			case cmd.pair != nil && *cmd.pair:
				if attempt != nil {
					cmd.done <- errors.New("ナビと接続中です")
					break
				}
				err := c.Waiting(ctx, 2)
				if err == nil {
					pairUntil = time.Now().Add(btPairWindow)
					m.mu.Lock()
					m.st.PairingUntil = &pairUntil
					m.mu.Unlock()
					m.set("pairing", "ナビのBluetooth設定から本体を選んでください")
				}
				cmd.done <- err
			case cmd.pair != nil:
				endPairing()
				m.set("idle", "")
				cmd.done <- nil
			}
		case e, ok := <-events:
			if !ok {
				return errors.New("純正Bluetoothのイベントが途切れました")
			}
			if !e.Manager() {
				continue
			}
			pairing := !pairUntil.IsZero()
			if stockbt.IsConfirmRequest(e.Kind) && (pairing || attempt != nil && attempt.addr == e.Addr) {
				// The stock app answers NG about 70 ms later; answer first.
				if err := c.ReplyConfirm(ctx, e.Addr, true); err != nil {
					m.fail(err)
				}
				if attempt != nil {
					attempt.repaired = true
				}
			}
			if e.Kind == stockbt.KindPairingDone && pairing {
				if k, err := learnKey(ctx, e.Addr); err != nil {
					m.fail(err)
				} else if err = m.remember(e.Addr, k); err != nil {
					m.fail(err)
				} else {
					m.s.cfg.Logf("android auto bluetooth: paired %s", e.Addr)
					endPairing()
					m.set("idle", "ナビを登録しました")
					nextTry = time.Now()
				}
			}
		case <-tick.C:
			if _, err := os.Stat("/proc/" + strconv.FormatUint(uint64(pid), 10)); err != nil {
				return errors.New("純正Bluetoothのプロセスが再起動しました")
			}
			if !m.wanted() {
				endPairing()
				if attempt != nil {
					attempt.cancel()
					<-attempt.done
				}
				return nil
			}
			if !pairUntil.IsZero() && time.Now().After(pairUntil) {
				endPairing()
				m.set("idle", "ペアリングの受付を終了しました")
			}
			if attempt != nil {
				select {
				case <-attempt.done:
					nextTry = time.Now().Add(attempt.retry)
					attempt = nil
					if pairUntil.IsZero() {
						m.set("idle", "")
					}
				default:
				}
			}
			if attempt == nil && pairUntil.IsZero() && time.Now().After(nextTry) && !m.s.active.Load() {
				if a, ok := m.nextHeadUnit(); ok {
					attempt = m.start(ctx, c, a)
				}
			}
		}
	}
}

// nextHeadUnit picks the head unit to try, in turn when several are paired.
func (m *btManager) nextHeadUnit() (stockbt.Addr, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.store) == 0 {
		return stockbt.Addr{}, false
	}
	m.next = (m.next + 1) % len(m.store)
	a, _, ok := parseStored(m.store[m.next])
	return a, ok
}

type btAttempt struct {
	addr     stockbt.Addr
	cancel   context.CancelFunc
	done     chan struct{}
	retry    time.Duration
	repaired bool
}

func (m *btManager) start(ctx context.Context, c *stockbt.Client, a stockbt.Addr) *btAttempt {
	actx, cancel := context.WithCancel(ctx)
	at := &btAttempt{addr: a, cancel: cancel, done: make(chan struct{}), retry: btRetry}
	go func() {
		defer close(at.done)
		defer cancel()
		if err := m.connect(actx, c, at); err != nil && actx.Err() == nil {
			m.fail(err)
		}
	}()
	return at
}

// connect brings up the projection RFCOMM channel of one head unit and runs
// the WiFi handshake and the projection session on it.
func (m *btManager) connect(ctx context.Context, c *stockbt.Client, at *btAttempt) error {
	events, stop := c.Subscribe(64)
	defer stop()
	spp := c.OpenSPP(btSlot)
	defer spp.Close()
	m.set("connecting", "ナビを呼び出しています")
	m.mu.Lock()
	m.st.Peer, m.st.Network = at.addr.String(), ""
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.st.Peer = ""
		m.mu.Unlock()
	}()
	if err := c.Connect(ctx, at.addr, btProfile); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = c.Disconnect(cleanup, at.addr, btProfile)
	}()
	limit := time.NewTimer(btConnectLimit)
	defer limit.Stop()
wait:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-limit.C:
			return nil // the head unit is off or out of range: try again later
		case e, ok := <-events:
			if !ok {
				return errors.New("純正Bluetoothのイベントが途切れました")
			}
			if p, st, ok := stockbt.ConnectState(e); ok && p == btProfile && e.Addr == at.addr {
				switch st {
				case stockbt.StateConnected:
					break wait
				case stockbt.StateConnecting:
				default:
					return nil
				}
			}
		}
	}
	m.seen(at.addr)
	if at.repaired {
		// The head unit asked to pair again: keep the new key.
		if k, err := learnKey(ctx, at.addr); err == nil {
			_ = m.remember(at.addr, k)
		}
	}
	m.set("handshake", "ナビからWi-Fiの情報を受け取っています")
	hctx, cancel := context.WithTimeout(ctx, btHandshake)
	var release func()
	start, err := aap.PhoneWireless(hctx, spp, func(jctx context.Context, cr aap.WifiCredentials) (string, error) {
		m.mu.Lock()
		m.st.Network = cr.SSID
		m.mu.Unlock()
		if m.s.cfg.JoinWiFi == nil {
			return "", ErrNoWiFiJoin
		}
		m.set("joining", "ナビのWi-Fiに接続しています")
		ip, rel, err := m.s.cfg.JoinWiFi(jctx, cr)
		release = rel
		return ip, err
	}, m.s.cfg.Logf)
	cancel()
	if release != nil {
		defer release()
	}
	if err != nil {
		if errors.Is(err, ErrNoWiFiJoin) {
			at.retry = btRetryNoJoin
		}
		return err
	}
	// The head unit keeps the RFCOMM channel for keepalives during projection.
	kctx, kcancel := context.WithCancel(ctx)
	defer kcancel()
	go func() { _ = aap.KeepWireless(kctx, spp) }()
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(start.IP, strconv.Itoa(start.Port)))
	if err != nil {
		return fmt.Errorf("ナビの投影ポートに接続できません: %w", err)
	}
	if !m.s.active.CompareAndSwap(false, true) {
		conn.Close()
		return errors.New("別のナビへ投影中です")
	}
	m.set("projecting", "ナビへ投影しています")
	m.s.sessions.Add(1)
	defer m.s.sessions.Done()
	defer m.s.active.Store(false)
	m.s.session(ctx, conn)
	return nil
}
