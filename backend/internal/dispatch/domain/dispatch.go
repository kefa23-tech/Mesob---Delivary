package domain

type CourierID string
type OrderID string
type Tier int

const (
	TierNearby Tier = iota
	TierPreferred
	TierExtended
)

type Courier struct {
	ID        CourierID
	Tier      Tier
	Available bool
}
type Assignment struct {
	Order   OrderID
	Courier CourierID
}
