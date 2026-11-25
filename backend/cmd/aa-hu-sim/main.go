// aa-hu-sim is a minimal Android Auto head unit for testing NP1's projection
// from a PC: it connects to the phone side (NP1, port 5277 through the hotspot
// or "adb forward"), acknowledges every frame and writes the H.264 stream.
//
//	go run ./cmd/aa-hu-sim -addr 127.0.0.1:5277 -out ../tmp/aa.h264 -seconds 20
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/np1-control-center/ncc/internal/androidauto/aap"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:5277", "phone projection server")
	out := flag.String("out", "aa.h264", "H.264 Annex B output")
	seconds := flag.Int("seconds", 20, "session length")
	mode := flag.String("mode", "800x480", "offered mode: 800x480, 1280x720 or 1920x1080")
	window := flag.Int("max-unacked", 4, "flow-control window")
	flag.Parse()
	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	c, err := net.DialTimeout("tcp", *addr, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	modes := map[string][3]int{"800x480": {800, 480, 30}, "1280x720": {1280, 720, 30}, "1920x1080": {1920, 1080, 30}}
	m, ok := modes[*mode]
	if !ok {
		log.Fatal("unknown mode")
	}
	var frames, bytes, configs int
	var first time.Time
	var lastTS time.Duration
	hu := &aap.SimHeadUnit{Modes: [][3]int{m}, MaxUnacked: *window, Logf: log.Printf,
		OnVideo: func(config bool, ts time.Duration, data []byte) {
			if config {
				configs++
			} else {
				frames++
				lastTS = ts
				if first.IsZero() {
					first = time.Now()
				}
			}
			bytes += len(data)
			_, _ = f.Write(data)
		}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*seconds)*time.Second)
	defer cancel()
	start := time.Now()
	err = hu.Run(ctx, c)
	el := time.Since(first).Seconds()
	log.Printf("phone=%q mode=%s configs=%d frames=%d bytes=%d first-frame-after=%v fps=%.1f kbps=%.0f last-ts=%v err=%v",
		hu.Phone, *mode, configs, frames, bytes, first.Sub(start).Round(time.Millisecond), float64(frames)/el, float64(bytes*8)/el/1000, lastTS, err)
}
