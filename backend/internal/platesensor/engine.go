package platesensor

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/np1-control-center/ncc/internal/plate"
)

// Engine wire protocol (stdin/stdout of the helper process):
//
//	request : "NPF1" | u32 width | u32 height | u32 format(1=NV12) | u64 seq | pixels
//	reply   : one JSON line {"seq":n,"ms":f,"plates":[...],"error":""}
//
// The helper first prints {"ready":true,"engine":"…","backend":"…"} or
// {"ready":false,"error":"…"} and must not print anything else on stdout.
const (
	protocolMagic = "NPF1"
	formatNV12    = 1
	maxReplyBytes = 256 * 1024
)

type EnginePlate struct {
	Box     [4]int        `json:"box"`
	Score   float64       `json:"score"`
	Reading plate.Reading `json:"reading"`
}

type EngineReply struct {
	Seq    uint64        `json:"seq"`
	MS     float64       `json:"ms"`
	Plates []EnginePlate `json:"plates"`
	Error  string        `json:"error"`
}

type EngineInfo struct {
	Ready   bool   `json:"ready"`
	Engine  string `json:"engine"`
	Backend string `json:"backend"`
	Error   string `json:"error"`
}

type Engine interface {
	Info() EngineInfo
	Process(ctx context.Context, seq uint64, nv12 []byte, width, height int) (EngineReply, error)
	Close() error
}

type processEngine struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  *bufio.Reader
	info   EngineInfo
	mu     sync.Mutex
	header [24]byte
}

// StartProcessEngine launches the helper and waits for its ready line.
func StartProcessEngine(ctx context.Context, path string, args []string, startup time.Duration) (Engine, error) {
	cmd := exec.Command(path, args...)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	p := &processEngine{cmd: cmd, stdin: stdin, lines: bufio.NewReaderSize(stdout, 64*1024)}
	type result struct {
		info EngineInfo
		err  error
	}
	ready := make(chan result, 1)
	go func() {
		line, e := readLine(p.lines)
		var info EngineInfo
		if e == nil {
			e = json.Unmarshal(line, &info)
		}
		ready <- result{info, e}
	}()
	select {
	case r := <-ready:
		if r.err != nil || !r.info.Ready {
			p.Close()
			if r.err == nil {
				r.err = errors.New(r.info.Error)
			}
			return nil, fmt.Errorf("engine not ready: %w", r.err)
		}
		p.info = r.info
		return p, nil
	case <-time.After(startup):
		p.Close()
		return nil, errors.New("engine startup timed out")
	case <-ctx.Done():
		p.Close()
		return nil, ctx.Err()
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		chunk, prefix, e := r.ReadLine()
		if e != nil {
			return nil, e
		}
		out = append(out, chunk...)
		if len(out) > maxReplyBytes {
			return nil, errors.New("engine reply too large")
		}
		if !prefix {
			return out, nil
		}
	}
}

func (p *processEngine) Info() EngineInfo { return p.info }

func (p *processEngine) Process(ctx context.Context, seq uint64, nv12 []byte, width, height int) (EngineReply, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	copy(p.header[0:4], protocolMagic)
	binary.LittleEndian.PutUint32(p.header[4:], uint32(width))
	binary.LittleEndian.PutUint32(p.header[8:], uint32(height))
	binary.LittleEndian.PutUint32(p.header[12:], formatNV12)
	binary.LittleEndian.PutUint64(p.header[16:], seq)
	type result struct {
		reply EngineReply
		err   error
	}
	done := make(chan result, 1)
	go func() {
		if _, e := p.stdin.Write(p.header[:]); e != nil {
			done <- result{err: e}
			return
		}
		if _, e := p.stdin.Write(nv12[:width*height*3/2]); e != nil {
			done <- result{err: e}
			return
		}
		line, e := readLine(p.lines)
		var reply EngineReply
		if e == nil {
			e = json.Unmarshal(line, &reply)
		}
		if e == nil && reply.Seq != seq {
			e = fmt.Errorf("engine replied to frame %d, expected %d", reply.Seq, seq)
		}
		done <- result{reply, e}
	}()
	select {
	case r := <-done:
		return r.reply, r.err
	case <-ctx.Done():
		// The protocol position is unknown now; the caller must restart us.
		p.kill()
		return EngineReply{}, ctx.Err()
	}
}

func (p *processEngine) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *processEngine) Close() error {
	_ = p.stdin.Close()
	exited := make(chan error, 1)
	go func() { exited <- p.cmd.Wait() }()
	select {
	case e := <-exited:
		return e
	case <-time.After(2 * time.Second):
		p.kill()
		return <-exited
	}
}
