package domain

type OrderID string
type ProductID string

type LineItem struct {
	Product        ProductID
	Quantity       int
	UnitPriceCents int64
}
type Order struct {
	ID         OrderID
	Items      []LineItem
	TotalCents int64
}

func (o Order) Total() int64 {
	var total int64
	for _, item := range o.Items {
		total += int64(item.Quantity) * item.UnitPriceCents
	}
	return total
}
