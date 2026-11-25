package aap

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestSessionStreamsToHeadUnit(t *testing.T) {
	cert, custom, err := LoadOrCreateCert(t.TempDir())
	if err != nil || custom {
		t.Fatalf("cert: %v %v", err, custom)
	}
	phone, car := tcpPair(t)
	var mu sync.Mutex
	var configs, frames [][]byte
	hu := &SimHeadUnit{
		Modes:      [][3]int{{800, 480, 30}, {1280, 720, 30}, {1920, 1080, 30}},
		MaxUnacked: 2,
		AckDelay:   5 * time.Millisecond,
		OnVideo: func(config bool, ts time.Duration, data []byte) {
			mu.Lock()
			defer mu.Unlock()
			if config {
				configs = append(configs, append([]byte(nil), data...))
			} else {
				frames = append(frames, append([]byte(nil), data...))
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	huDone := make(chan error, 1)
	go func() { huDone <- hu.Run(ctx, car) }()

	touches := make(chan Touch, 4)
	s, err := Connect(ctx, phone, Options{TLS: ServerTLSConfig(cert), PhoneName: "NP1", PhoneBrand: "Test", MaxWidth: 1280, MaxHeight: 720, OnTouch: func(tc Touch) { touches <- tc }})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if s.Chosen.Width != 1280 || s.Chosen.Height != 720 || s.Chosen.Index != 1 {
		t.Fatalf("chose %+v", s.Chosen)
	}
	if s.HU.Make != "Sim" || s.HU.TouchWidth != 800 {
		t.Fatalf("head unit %+v", s.HU)
	}
	if err = s.WaitFocus(ctx); err != nil {
		t.Fatalf("focus: %v", err)
	}
	sps := []byte{0, 0, 0, 1, 0x67, 1, 2, 3, 0, 0, 0, 1, 0x68, 4}
	if err = s.SendCodecConfig(ctx, sps); err != nil {
		t.Fatal(err)
	}
	// One large frame (fragmented) and several small ones through a window of 2.
	big := bytes.Repeat([]byte{0xab}, 70000)
	big[0], big[1], big[2], big[3], big[4] = 0, 0, 0, 1, 0x65
	if err = s.SendFrame(ctx, 0, big); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10; i++ {
		if err = s.SendFrame(ctx, time.Duration(i)*33*time.Millisecond, []byte{0, 0, 0, 1, 0x41, byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(frames)
		mu.Unlock()
		if n == 11 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if len(configs) != 1 || !bytes.Equal(configs[0], sps) {
		t.Fatalf("config %x", configs)
	}
	if len(frames) != 11 || !bytes.Equal(frames[0], big) || frames[10][5] != 10 {
		t.Fatalf("got %d frames", len(frames))
	}
	mu.Unlock()
	if st := s.Stats(); st.Frames != 11 || st.MaxUnacked != 2 {
		t.Fatalf("stats %+v", st)
	}
	s.Close()
	if err = <-huDone; err != nil {
		t.Fatalf("head unit: %v", err)
	}
	if hu.Phone != "NP1 / Test" || hu.Chosen != 1 {
		t.Fatalf("head unit saw %q index %d", hu.Phone, hu.Chosen)
	}
}

func TestTouchReportParses(t *testing.T) {
	m, err := pbDecode(touchReport(0, 120, 45))
	if err != nil {
		t.Fatal(err)
	}
	te, ok := m.sub(3)
	if !ok {
		t.Fatal("no touch")
	}
	p, _ := pbDecode(te.all(1)[0])
	x, _ := p.uint(1)
	y, _ := p.uint(2)
	if x != 120 || y != 45 {
		t.Fatalf("%d,%d", x, y)
	}
}

func TestPhoneWireless(t *testing.T) {
	phone, car := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		var w pbWriter
		w.uint(1, 1)
		w.uint(2, 1)
		writeWPP(car, wppVersionRequest, w.b)
		readWPP(car) // version response
		w = pbWriter{}
		w.string(1, "192.168.50.1")
		w.uint(2, 5288)
		writeWPP(car, wppStartRequest, w.b)
		if typ, _, _ := readWPP(car); typ != wppInfoRequest {
			t.Errorf("want info request, got %d", typ)
		}
		w = pbWriter{}
		w.string(1, "CarNet")
		w.string(2, "secretpass")
		w.string(3, "00:11:22:33:44:55")
		w.uint(4, 8)
		w.uint(5, 1)
		writeWPP(car, wppInfoResponse, w.b)
		readWPP(car)
		readWPP(car)
	}()
	var got WifiCredentials
	start, err := PhoneWireless(ctx, phone, func(_ context.Context, c WifiCredentials) (string, error) {
		got = c
		return "192.168.50.20", nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if start.IP != "192.168.50.1" || start.Port != 5288 || got.SSID != "CarNet" || got.Key != "secretpass" || got.Security != 8 {
		t.Fatalf("%+v %+v", start, got)
	}
}

func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	acc := make(chan net.Conn, 1)
	go func() {
		c, _ := l.Accept()
		acc <- c
	}()
	a, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b := <-acc
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}
