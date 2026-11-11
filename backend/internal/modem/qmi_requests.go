package modem

import (
	"errors"
	"fmt"
)

type qmiProtocolError struct {
	Message, Result, Code uint16
	Extended              *uint16
}

func (e *qmiProtocolError) Error() string {
	if e.Extended != nil {
		return fmt.Sprintf("QMI Get %04x failed: result=%d code=%d extended=%d", e.Message, e.Result, e.Code, *e.Extended)
	}
	return fmt.Sprintf("QMI Get %04x failed: result=%d code=%d", e.Message, e.Result, e.Code)
}

// fixedQMIRequest is internal and cannot build arbitrary QMI operations. The
// transaction/message pair selects the entire request, including profile IDs.
func fixedQMIRequest(transaction, message uint16) ([]byte, error) {
	switch {
	case transaction == 1 && message == 0x004f:
		return signalRequest(1), nil
	case transaction == 2 && message == 0x0024, transaction == 3 && message == 0x0031:
		return []byte{0, byte(transaction), 0, byte(message), 0, 0, 0}, nil
	case transaction == 4 && message == 0x002d:
		return apnCurrentRequest(), nil
	case transaction == 5 && message == 0x002b:
		return profileRequest(1)
	case transaction == 6 && message == 0x002b:
		return profileRequest(2)
	case transaction == 7 && message == 0x002a:
		return profileInventoryRequest(), nil
	default:
		return nil, errors.New("QMI query not allowlisted")
	}
}
