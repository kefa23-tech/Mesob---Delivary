package domain

import "time"

type Status string

const StatusOK Status = "ok"

type Report struct {
	Status    Status    `json:"status"`
	Service   string    `json:"service"`
	Timestamp time.Time `json:"timestamp"`
}
