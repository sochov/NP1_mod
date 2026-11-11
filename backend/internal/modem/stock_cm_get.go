package modem

import (
	"context"
	"errors"
	"reflect"
	"sort"
)

type stockCMGuardedExchange struct {
	service  uint32
	verify   func() error
	exchange func(context.Context, uint16, uint16) ([]byte, error)
	failure  error
}

func (e *stockCMGuardedExchange) run(ctx context.Context, transaction, message uint16) ([]byte, error) {
	if e.failure != nil {
		return nil, e.failure
	}
	if !stockCMFixedGetAllowed(e.service, transaction, message) {
		return nil, errors.New("STOCK_CM_GET_QUERY_NOT_ALLOWED")
	}
	if e.failure = e.verify(); e.failure != nil {
		return nil, e.failure
	}
	frame, err := e.exchange(ctx, transaction, message)
	if e.failure = e.verify(); e.failure != nil {
		return nil, e.failure
	}
	return frame, err
}

type stockCMDescriptor struct {
	pid uint32
	fd  int
}

// A private process identity snapshot, never a reusable public authorization.
type stockCMLease struct {
	owner, cm   stockCMProcess
	descriptors []int
}

func (stockCMLease) String() string     { return "[private stock CM read-only lease]" }
func (v stockCMLease) GoString() string { return v.String() }

func stockCMDescriptorSet(cmPID, selfPID uint32, ownFD int, rows []stockCMDescriptor) ([]int, error) {
	if cmPID == 0 || selfPID == 0 || cmPID == selfPID || ownFD < -1 || len(rows) > 65536 {
		return nil, errors.New("STOCK_CM_GET_DESCRIPTOR_INPUT_INVALID")
	}
	cm := []int{}
	self := 0
	seen := map[stockCMDescriptor]bool{}
	for _, row := range rows {
		if row.pid == 0 || row.fd < 0 || row.fd > 1<<20 || seen[row] {
			return nil, errors.New("STOCK_CM_GET_DESCRIPTOR_INPUT_INVALID")
		}
		seen[row] = true
		switch {
		case row.pid == cmPID:
			cm = append(cm, row.fd)
		case row.pid == selfPID && ownFD >= 0 && row.fd == ownFD:
			self++
		default:
			return nil, errors.New("STOCK_CM_GET_FOREIGN_QMI_OWNER")
		}
	}
	if len(cm) == 0 || len(cm) > 32 || ownFD >= 0 && self != 1 || ownFD == -1 && self != 0 {
		return nil, errors.New("STOCK_CM_GET_DESCRIPTOR_SET_UNVERIFIED")
	}
	sort.Ints(cm)
	return cm, nil
}

func stockCMLeaseMatches(before, after stockCMLease) bool {
	return stockCMProcessUnchanged(before.owner, after.owner) && stockCMProcessUnchanged(before.cm, after.cm) && reflect.DeepEqual(before.descriptors, after.descriptors)
}

func stockCMFixedGetAllowed(service uint32, transaction, message uint16) bool {
	return service == 3 && (transaction == 1 && message == 0x4f || transaction == 2 && message == 0x24 || transaction == 3 && message == 0x31) ||
		service == 1 && (transaction == 5 && message == 0x2b || transaction == 6 && message == 0x2b || transaction == 7 && message == 0x2a)
}
