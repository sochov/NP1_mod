package model

import "time"

type Observation struct {
	Status         string    `json:"status"`
	ObservedAt     time.Time `json:"observedAt"`
	Source         string    `json:"source"`
	RefreshSeconds int       `json:"refreshSeconds"`
	Verified       bool      `json:"verified"`
	Data           any       `json:"data"`
	Reason         string    `json:"reason,omitempty"`
}

func Value(source string, data any) Observation {
	return Observation{"available", time.Now().UTC(), source, 10, true, data, ""}
}
func Missing(source, reason string) Observation {
	return Observation{"unknown", time.Now().UTC(), source, 10, false, nil, reason}
}

type Capability struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Writable bool   `json:"writable"`
}
