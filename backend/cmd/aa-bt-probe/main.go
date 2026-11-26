// aa-bt-probe calls one function of a stock Bluetooth handler on the NP1 stock
// D-Bus and prints the reply and the events that follow (investigation tool).
//
//	aa-bt-probe -handler NPBTManagerHandler -func 0x21 -resp 8 -payload hex -seconds 3
package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
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

func main() {
	handler := flag.String("handler", "NPBTManagerHandler", "stock handler")
	fn := flag.Uint("func", 0, "function id")
	resp := flag.Uint("resp", 1, "reply length")
	payloadHex := flag.String("payload", "", "payload hex (use {seq} placeholder not supported; give literal)")
	seconds := flag.Float64("seconds", 3, "event watch time")
	listen := flag.Bool("listen-only", false, "only print events")
	showMax := flag.Int("show", 160, "bytes of each event to print")
	autoConfirm := flag.Bool("auto-confirm", false, "accept SSP numeric-comparison requests (manager event 0x40d) as soon as they arrive")
	flag.Parse()
	conn, err := dbus.Connect(busAddress())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	name := "SPF.EventBase." + *handler + ".Stub"
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
	mgr := conn.Object(owner, dbus.ObjectPath("/SPF/EventBase/"+*handler))
	seq := uint32(100)
	if !*listen {
		payload, err := hex.DecodeString(*payloadHex)
		if err != nil {
			log.Fatal(err)
		}
		obj := conn.Object(owner, dbus.ObjectPath("/SPF/EventBase/"+*handler))
		var result []byte
		var ret []uint32
		call := obj.Call("SPF.EventBase.v1_0.CallFunction", 0, uint32(1), caller, uint32(*fn), []uint32{}, payload, uint32(*resp))
		if err = call.Store(&result, &ret); err != nil {
			fmt.Println("call error:", err)
		}
		fmt.Printf("caller %d reply %s ret %v\n", caller, hex.EncodeToString(result), ret)
	}
	deadline := time.After(time.Duration(*seconds * float64(time.Second)))
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
			if *autoConfirm && s.Path == "/SPF/EventBase/Evt042" && len(data) >= 0x36 && binary.LittleEndian.Uint32(data[0x2c:]) == 0x40d {
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
				fmt.Printf("%s auto-confirm %x reply %x err %v\n", time.Now().Format("15:04:05.000"), data[0x30:0x36], result, err)
			}
			if *listen || to == caller {
				id := uint32(0)
				if len(data) >= 12 {
					id = binary.LittleEndian.Uint32(data[8:12])
				}
				show := data
				if len(show) > *showMax {
					show = show[:*showMax]
				}
				fmt.Printf("%s %s to=%d %v event=0x%x len=%d %s\n", time.Now().Format("15:04:05.000"), s.Path, to, s.Body[1], id, len(data), hex.EncodeToString(show))
			}
		}
	}
}
