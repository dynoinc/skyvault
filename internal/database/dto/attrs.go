package dto

import "time"

type state string

const (
	StateCommitted state = "COMMITTED"
	StateMerging   state = "MERGING"
	StateMerged    state = "MERGED"
)

type L0BatchAttrs struct {
	State     state     `json:"state"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	SizeBytes int64     `json:"size_bytes"`
	MinKey    string    `json:"min_key"`
	MaxKey    string    `json:"max_key"`
}
