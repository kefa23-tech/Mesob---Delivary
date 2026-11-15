package domain

import "time"

type DeadLetterTask struct {
	ID             string    `json:"id"`
	Queue          string    `json:"queue"`
	Type           string    `json:"type"`
	Payload        string    `json:"payload"`
	PayloadSummary string    `json:"payload_summary"`
	LastError      string    `json:"last_error"`
	RetryCount     int       `json:"retry_count"`
	LastFailedAt   time.Time `json:"last_failed_at"`
}

type CourierID string
type DisputeID string
type ReviewStatus string

const (
	Pending  ReviewStatus = "pending"
	Approved ReviewStatus = "approved"
	Rejected ReviewStatus = "rejected"
)

type KYCReview struct {
	Courier CourierID
	Status  ReviewStatus
}
type Dispute struct {
	ID         DisputeID
	Status     ReviewStatus
	Resolution string
}
