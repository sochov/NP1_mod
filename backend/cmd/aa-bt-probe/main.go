// aa-bt-probe calls the stock Bluetooth handlers on the NP1 stock D-Bus and
// prints the replies and the events that follow (investigation tool).
//
//	aa-bt-probe -handler NPBTManagerHandler -func 0x21 -resp 8 -payload hex -seconds 3
//	aa-bt-probe -listen-only -auto-confirm -seconds 60
//	aa-bt-probe -slot 2                               # AA UUID into VSSPP3 (memory only)
//	aa-bt-probe -connect 64:49:7D:E0:DB:42 -profile 0x0c -auto-confirm -spp-send 0102
//	aa-bt-probe -connect 64:49:7D:E0:DB:42 -profile 0x0c -auto-confirm -wpp
//	aa-bt-probe -linkkey 64:49:7D:E0:DB:42=00112233445566778899aabbccddeeff
//	aa-bt-probe -find-linkkey 64:49:7D:E0:DB:42
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/np1-control-center/ncc/internal/androidauto/aap"
	"github.com/np1-control-center/ncc/internal/androidauto/stockbt"
)

func busAddress() string {
	b, err := os.ReadFile("/tmp/dbus_session_bus_address")
	if err != nil {
		log.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "DBUS_SESSION_BUS_ADDRESS='"); ok {
			return strings.SplitN(v, "'", 2)[0]
		}
	}
	log.Fatal("no bus address")
	return ""
}

// aaLocalUUID is what NP1 publishes for the slot; head units only look for
// their own UUID (the remote one), so any fixed value works.
const aaLocalUUID = "a3b1c4d0-6e2f-4b7a-9c1d-2f5e8a7b0c13"

func main() {
	handler := flag.String("handler", "NPBTManagerHandler", "stock handler")
	fn := flag.Uint("func", 0, "function id")
	resp := flag.Uint("resp", 1, "reply length")
	payloadHex := flag.String("payload", "", "payload hex (literal)")
	seconds := flag.Float64("seconds", 3, "event watch time")
	listen := flag.Bool("listen-only", false, "only print events")
	showMax := flag.Int("show", 160, "bytes of each event to print")
	autoConfirm := flag.Bool("auto-confirm", false, "accept SSP numeric-comparison requests (manager events 0x40d, 0x1405) as soon as they arrive")
	slot := flag.Int("slot", -1, "write the Android Auto UUID into this vendor SPP slot (0-based, VSSPP3=2) of the running stock stack, then exit")
	connect := flag.String("connect", "", "connect this head unit address (with -profile)")
	profile := flag.Uint("profile", stockbt.ProfileVSSPP3, "profile code for -connect")
	sppSend := flag.String("spp-send", "", "hex bytes to write on the profile once connected")
	wpp := flag.Bool("wpp", false, "run the phone side of the wireless handshake on the profile once connected (joins nothing)")
	linkkey := flag.String("linkkey", "", "ADDR=KEYHEX: hand the stack a link key, then exit")
	findKey := flag.String("find-linkkey", "", "print the newest link key of ADDR in the stock HCI log, then exit")
	flag.Parse()

	if *findKey != "" {
		a, err := stockbt.ParseAddr(*findKey)
		if err != nil {
			log.Fatal(err)
		}
		k, err := stockbt.FindLinkKey(stockbt.SnoopLog, a)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s key %x type %d\n", a, k.Key, k.Type)
		return
	}

	if *slot >= 0 {
		c, err := stockbt.Dial(context.Background())
		if err != nil {
			log.Fatal(err)
		}
		st, err := stockbt.SetVendorSlot(c.PID(), *slot, aaLocalUUID, aap.WirelessUUID)
		c.Close()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("slot %d valid local %x remote %x\n", st.Slot, st.Local, st.Remote)
		return
	}
	if *linkkey != "" {
		a, k, ok := strings.Cut(*linkkey, "=")
		addr, err := stockbt.ParseAddr(a)
		kb, err2 := hex.DecodeString(k)
		if !ok || err != nil || err2 != nil || len(kb) != 16 {
			log.Fatal("-linkkey ADDR=32 hex digits")
		}
		c, err := stockbt.Dial(context.Background())
		if err != nil {
			log.Fatal(err)
		}
		defer c.Close()
		var key [16]byte
		copy(key[:], kb)
		fmt.Println("set link key:", c.SetLinkkey(context.Background(), addr, key))
		return
	}
	if *connect != "" {
		runConnect(*connect, uint32(*profile), *autoConfirm, *sppSend, *wpp, *seconds)
		return
	}
	runRaw(*handler, uint32(*fn), uint32(*resp), *payloadHex, *seconds, *listen, *showMax, *autoConfirm)
}

func stamp() string { return time.Now().Format("15:04:05.000") }

// runConnect connects one profile of a head unit and then exercises it.
func runConnect(addrText string, profile uint32, autoConfirm bool, sppSend string, wpp bool, seconds float64) {
	addr, err := stockbt.ParseAddr(addrText)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds*float64(time.Second)))
	defer cancel()
	c, err := stockbt.Dial(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	events, stop := c.Subscribe(256)
	defer stop()
	spp := c.OpenSPP(profile)
	defer spp.Close()
	if err = c.Connect(ctx, addr, profile); err != nil {
		log.Fatal(err)
	}
	fmt.Println(stamp(), "connect requested")
	connected := make(chan struct{})
	go func() {
		once := false
		for e := range events {
			switch {
			case e.Manager():
				fmt.Printf("%s manager kind=%#x addr=%s %x\n", stamp(), e.Kind, e.Addr, e.Data[0x2c:min(len(e.Data), 0x60)])
				if autoConfirm && (e.Kind == stockbt.KindConfirmIncoming || e.Kind == stockbt.KindConfirmOutgoing) {
					fmt.Println(stamp(), "confirm:", c.ReplyConfirm(ctx, e.Addr, true))
				}
				if e.Kind == stockbt.KindPairingDone || e.Kind == stockbt.KindPairingFailed {
					fmt.Printf("%s pairing event body %x\n", stamp(), e.Data)
				}
			default:
				if st, ok := stockbt.ParseSPPStatus(e); ok {
					fmt.Printf("%s spp status flag=%d status=%#x profile=%#x %x\n", stamp(), st.Flag, st.Status, st.Profile, e.Data)
					if !once && st.Profile == profile {
						once = true
						close(connected)
					}
				} else if d, ok := stockbt.ParseSPPData(e); ok {
					fmt.Printf("%s spp data profile=%#x len=%d %x (header %x)\n", stamp(), d.Profile, len(d.Data), d.Data, e.Data[:0x40])
				} else {
					fmt.Printf("%s event %#x %x\n", stamp(), e.ID, e.Data[:min(len(e.Data), 0x80)])
				}
			}
		}
	}()
	if sppSend == "" && !wpp {
		<-ctx.Done()
		return
	}
	select {
	case <-connected:
	case <-ctx.Done():
		fmt.Println(stamp(), "no SPP status event for the profile")
		return
	}
	if sppSend != "" {
		b, err := hex.DecodeString(sppSend)
		if err != nil {
			log.Fatal(err)
		}
		_, err = spp.Write(b)
		fmt.Println(stamp(), "spp write:", err)
	}
	if wpp {
		start, err := aap.PhoneWireless(ctx, spp, func(_ context.Context, cr aap.WifiCredentials) (string, error) {
			fmt.Printf("%s head unit network ssid=%q bssid=%s security=%d apType=%d key=%d bytes\n", stamp(), cr.SSID, cr.BSSID, cr.Security, cr.APType, len(cr.Key))
			return "192.0.2.10", nil // test address: nothing is joined
		}, func(f string, a ...any) { fmt.Println(stamp(), fmt.Sprintf(f, a...)) })
		fmt.Printf("%s handshake done: %+v err=%v\n", stamp(), start, err)
	}
	<-ctx.Done()
}

func runRaw(handler string, fn, resp uint32, payloadHex string, seconds float64, listen bool, showMax int, autoConfirm bool) {
	conn, err := dbus.Connect(busAddress())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	name := "SPF.EventBase." + handler + ".Stub"
	var owner string
	if err = conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner); err != nil {
		log.Fatal(err)
	}
	var pid uint32
	_ = conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, owner).Store(&pid)
	exe, _ := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	fmt.Printf("owner %s pid %d %s\n", owner, pid, exe)
	if err = conn.AddMatchSignal(dbus.WithMatchInterface("SPF.EventBase.v1_0"), dbus.WithMatchMember("SPFEvent")); err != nil {
		log.Fatal(err)
	}
	signals := make(chan *dbus.Signal, 256)
	conn.Signal(signals)
	caller := uint32(os.Getpid())
	mgr := conn.Object(owner, dbus.ObjectPath("/SPF/EventBase/"+handler))
	seq := uint32(100)
	if !listen {
		payload, err := hex.DecodeString(payloadHex)
		if err != nil {
			log.Fatal(err)
		}
		var result []byte
		var ret []uint32
		call := mgr.Call("SPF.EventBase.v1_0.CallFunction", 0, uint32(1), caller, fn, []uint32{}, payload, resp)
		if err = call.Store(&result, &ret); err != nil {
			fmt.Println("call error:", err)
		}
		fmt.Printf("caller %d reply %s ret %v\n", caller, hex.EncodeToString(result), ret)
	}
	deadline := time.After(time.Duration(seconds * float64(time.Second)))
	for {
		select {
		case <-deadline:
			return
		case s := <-signals:
			if len(s.Body) != 3 {
				continue
			}
			to, _ := s.Body[0].(uint32)
			data, _ := s.Body[2].([]byte)
			if autoConfirm && s.Path == "/SPF/EventBase/Evt042" && len(data) >= 0x36 && isConfirmRequest(binary.LittleEndian.Uint32(data[0x2c:])) {
				// ReplyUserConfirmReq: header, BD address (+0x18), accept (+0x1e).
				seq++
				p := make([]byte, 0x20)
				binary.LittleEndian.PutUint32(p[0:], seq)
				binary.LittleEndian.PutUint32(p[8:], 0x12)
				binary.LittleEndian.PutUint32(p[12:], 0x11)
				binary.LittleEndian.PutUint64(p[16:], 1)
				copy(p[0x18:0x1e], data[0x30:0x36])
				p[0x1e] = 1
				var result []byte
				var ret []uint32
				err := mgr.Call("SPF.EventBase.v1_0.CallFunction", 0, uint32(1), caller, uint32(0x12), []uint32{}, p, uint32(1)).Store(&result, &ret)
				fmt.Printf("%s auto-confirm %x reply %x err %v\n", stamp(), data[0x30:0x36], result, err)
			}
			if listen || to == caller {
				id := uint32(0)
				if len(data) >= 12 {
					id = binary.LittleEndian.Uint32(data[8:12])
				}
				show := data
				if len(show) > showMax {
					show = show[:showMax]
				}
				fmt.Printf("%s %s to=%d %v event=0x%x len=%d %s\n", stamp(), s.Path, to, s.Body[1], id, len(data), hex.EncodeToString(show))
			}
		}
	}
}

// isConfirmRequest reports SSP numeric-comparison requests: 0x40d when the
// peer starts pairing, 0x1405 when NP1 starts it (outgoing connection).
func isConfirmRequest(kind uint32) bool {
	return kind == stockbt.KindConfirmIncoming || kind == stockbt.KindConfirmOutgoing
}
