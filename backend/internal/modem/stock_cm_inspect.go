package modem

import "time"

// This diagnostic reports provenance only. It does not authorize concurrent
// QMI clients and opens no modem device. The production ownership guard still
// refuses every foreign device FD until a separate fixed-GET field test passes.
type StockCMOwnershipProof struct {
	ObservedAt     time.Time `json:"observedAt"`
	Source         string    `json:"source"`
	Status         string    `json:"status"`
	OwnerVerified  bool      `json:"ownerVerified"`
	CMProcesses    int       `json:"cmProcesses"`
	QMIDescriptors int       `json:"qmiDescriptors"`
	Verified       bool      `json:"verified"`
}
