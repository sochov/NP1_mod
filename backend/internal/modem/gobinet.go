package modem

import (
	"context"
	"encoding/binary"
	"errors"
	"time"
)

const GobiNetDriverVersion = "Quectel_Linux&Android_GobiNet_Driver_V1.6.2.14"
const GobiNetModuleSHA256 = "cbe36e78b6a174283c9757c2e1fe6e1da242e8cc2c465f19160b5a1205f6db72"
const maximumQMIFrame = 4096

type GobiNetSignal struct {
	ObservedAt    time.Time         `json:"observedAt"`
	Source        string            `json:"source"`
	DriverVersion string            `json:"driverVersion"`
	RSSI          *float64          `json:"rssi"`
	RSRQ          *float64          `json:"rsrq"`
	RSRP          *float64          `json:"rsrp"`
	SNR           *float64          `json:"snr"`
	Registered    *bool             `json:"registered"`
	PSAttached    *bool             `json:"psAttached"`
	Carrier       string            `json:"carrier,omitempty"`
	PLMN          string            `json:"plmn,omitempty"`
	Band          string            `json:"band,omitempty"`
	Bands         []string          `json:"bands,omitempty"`
	RAT           string            `json:"rat,omitempty"`
	Queries       map[string]string `json:"queries"`
}

var errUnrelatedQMI = errors.New("unrelated QMI transaction")

func signalRequest(transaction uint16) []byte {
	return []byte{0, byte(transaction), byte(transaction >> 8), 0x4f, 0, 0, 0}
}
func measured(value, min, max float64) *float64 {
	if value < min || value > max {
		return nil
	}
	return &value
}

// parseSignalResponse accepts QMI service data only: GobiNet owns the six-byte
// QMUX envelope. LTE SNR is kept distinct from SINR throughout the API.
func parseSignalResponse(frame []byte, transaction uint16) (GobiNetSignal, error) {
	out := GobiNetSignal{Source: "gobinet-nas-independent-cid", DriverVersion: GobiNetDriverVersion}
	values, e := parseNASResponse(frame, transaction, 0x004f)
	if e != nil {
		return out, e
	}
	if value, ok := values[0x14]; ok {
		if len(value) != 6 {
			return out, errors.New("invalid LTE signal TLV")
		}
		out.RSSI = measured(float64(int8(value[0])), -125, -30)
		out.RSRQ = measured(float64(int8(value[1])), -40, -1)
		out.RSRP = measured(float64(int16(binary.LittleEndian.Uint16(value[2:4]))), -160, -30)
		out.SNR = measured(float64(int16(binary.LittleEndian.Uint16(value[4:6])))/10, -30, 50)
	}
	out.ObservedAt = time.Now().UTC()
	return out, nil
}

func parseNASResponse(frame []byte, transaction, message uint16) (map[byte][]byte, error) {
	return parseQMIResponse(frame, transaction, message)
}

func parseQMIResponse(frame []byte, transaction, message uint16) (map[byte][]byte, error) {
	out := map[byte][]byte{}
	if len(frame) < 7 || len(frame) > maximumQMIFrame {
		return out, errors.New("invalid QMI frame size")
	}
	length := int(binary.LittleEndian.Uint16(frame[5:7]))
	if length != len(frame)-7 {
		return out, errors.New("QMI payload length mismatch")
	}
	if frame[0] != 2 || binary.LittleEndian.Uint16(frame[1:3]) != transaction || binary.LittleEndian.Uint16(frame[3:5]) != message {
		return out, errUnrelatedQMI
	}
	seen := map[byte]bool{}
	success := false
	var operationError *qmiProtocolError
	for pos := 7; pos < len(frame); {
		if len(frame)-pos < 3 {
			return out, errors.New("truncated QMI TLV header")
		}
		kind := frame[pos]
		size := int(binary.LittleEndian.Uint16(frame[pos+1 : pos+3]))
		pos += 3
		if size > len(frame)-pos {
			return out, errors.New("truncated QMI TLV value")
		}
		value := frame[pos : pos+size]
		pos += size
		if seen[kind] {
			return out, errors.New("duplicate QMI TLV")
		}
		seen[kind] = true
		out[kind] = value
		switch kind {
		case 2:
			if size != 4 {
				return out, errors.New("invalid QMI operation result")
			}
			result, code := binary.LittleEndian.Uint16(value), binary.LittleEndian.Uint16(value[2:])
			if result != 0 || code != 0 {
				operationError = &qmiProtocolError{Message: message, Result: result, Code: code}
			}
			success = true
		}
	}
	if !success {
		return out, errors.New("QMI operation result missing")
	}
	if operationError != nil {
		if operationError.Code == 81 {
			if value, ok := out[0xe0]; ok {
				if len(value) != 2 {
					return out, errors.New("invalid QMI extended error length")
				}
				code := binary.LittleEndian.Uint16(value)
				operationError.Extended = &code
			}
		}
		return out, operationError
	}
	return out, nil
}

// collectNAS separates the three fixed query outcomes. An optional status
// failure cannot erase measured signal values or manufacture a registration.
func collectNAS(ctx context.Context, exchange func(context.Context, uint16, uint16) ([]byte, error)) (GobiNetSignal, error) {
	out := GobiNetSignal{Source: "gobinet-nas-independent-cid", DriverVersion: GobiNetDriverVersion, Queries: map[string]string{}}
	jobs := []struct {
		name                 string
		transaction, message uint16
	}{{"signal", 1, 0x004f}, {"servingSystem", 2, 0x0024}, {"rfBand", 3, 0x0031}}
	success := false
	for _, job := range jobs {
		if e := ctx.Err(); e != nil {
			out.Queries[job.name] = "timeout"
			continue
		}
		frame, e := exchange(ctx, job.transaction, job.message)
		if e == nil {
			switch job.message {
			case 0x004f:
				var signal GobiNetSignal
				signal, e = parseSignalResponse(frame, job.transaction)
				if e == nil {
					out.RSSI, out.RSRQ, out.RSRP, out.SNR = signal.RSSI, signal.RSRQ, signal.RSRP, signal.SNR
				}
			case 0x0024:
				var serving nasServing
				serving, e = parseServingResponse(frame, job.transaction)
				if e == nil {
					out.Registered, out.PSAttached, out.Carrier, out.PLMN, out.RAT = serving.Registered, serving.PSAttached, serving.Carrier, serving.PLMN, serving.RAT
				}
			case 0x0031:
				var bands []string
				var band string
				band, bands, e = parseBandResponse(frame, job.transaction)
				if e == nil {
					out.Band, out.Bands = band, bands
				}
			}
		}
		if e != nil {
			out.Queries[job.name] = "failed"
			if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
				out.Queries[job.name] = "timeout"
			}
			continue
		}
		out.Queries[job.name] = "available"
		out.ObservedAt = time.Now().UTC()
		success = true
	}
	if !success {
		return out, errors.New("no valid response to fixed NAS Get queries")
	}
	return out, nil
}

func collectSignalOnly(ctx context.Context, exchange func(context.Context, uint16, uint16) ([]byte, error)) (GobiNetSignal, error) {
	if e := ctx.Err(); e != nil {
		return GobiNetSignal{}, e
	}
	frame, e := exchange(ctx, 1, 0x004f)
	if e != nil {
		return GobiNetSignal{}, e
	}
	out, e := parseSignalResponse(frame, 1)
	if e != nil {
		return GobiNetSignal{}, e
	}
	out.Queries = map[string]string{"signal": "available", "servingSystem": "not-requested", "rfBand": "not-requested"}
	return out, nil
}
