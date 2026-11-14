package domain

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
