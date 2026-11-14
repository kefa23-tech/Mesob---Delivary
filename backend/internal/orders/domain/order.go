package domain

import (
	"errors"
	"sort"
)

const GroupDiscountThresholdMinor int64 = 120_000

var (
	ErrCartNotFound       = errors.New("group cart not found")
	ErrCartLocked         = errors.New("group cart is locked")
	ErrRestaurantMismatch = errors.New("cart only accepts items from its restaurant")
	ErrInvalidQuantity    = errors.New("quantity must be positive")
	ErrInvalidSelection   = errors.New("selected options do not satisfy the option group constraints")
	ErrItemNotFound       = errors.New("cart item not found")
	ErrInvalidToken       = errors.New("invalid group cart token")
)

type CartStatus string

const (
	CartOpen   CartStatus = "open"
	CartLocked CartStatus = "locked"
)

type Participant struct {
	UserID      string `json:"user_id,omitempty"`
	DisplayName string `json:"display_name"`
}

func (p Participant) Key() string {
	if p.UserID != "" {
		return "user:" + p.UserID
	}
	return "guest:" + p.DisplayName
}

type SelectedOption struct {
	GroupID         string `json:"group_id"`
	GroupName       string `json:"group_name"`
	OptionID        string `json:"option_id"`
	OptionName      string `json:"option_name"`
	PriceDeltaMinor int64  `json:"price_delta_minor"`
}
type CartItem struct {
	ID             string           `json:"id"`
	MenuItemID     string           `json:"menu_item_id"`
	RestaurantID   string           `json:"restaurant_id"`
	Name           string           `json:"name"`
	UnitPriceMinor int64            `json:"unit_price_minor"`
	Quantity       int              `json:"quantity"`
	Options        []SelectedOption `json:"options,omitempty"`
	Participant    Participant      `json:"participant"`
	LineTotalMinor int64            `json:"line_total_minor"`
}
type Option struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PriceDeltaMinor int64  `json:"price_delta_minor"`
	IsAvailable     bool   `json:"is_available"`
}
type OptionGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	MinSelect int      `json:"min_select"`
	MaxSelect int      `json:"max_select"`
	Options   []Option `json:"options"`
}
type MenuItem struct {
	ID           string        `json:"id"`
	RestaurantID string        `json:"restaurant_id"`
	Name         string        `json:"name"`
	PriceMinor   int64         `json:"price_minor"`
	OptionGroups []OptionGroup `json:"option_groups"`
}
type ParticipantSummary struct {
	Participant   Participant `json:"participant"`
	SubtotalMinor int64       `json:"subtotal_minor"`
	DeliveryMinor int64       `json:"delivery_fee_minor"`
	TotalMinor    int64       `json:"total_minor"`
}
type GroupCart struct {
	Token                 string               `json:"token"`
	RestaurantID          string               `json:"restaurant_id"`
	Host                  Participant          `json:"host"`
	Status                CartStatus           `json:"status"`
	Items                 []CartItem           `json:"items"`
	SubtotalMinor         int64                `json:"subtotal_minor"`
	DeliveryFeeMinor      int64                `json:"delivery_fee_minor"`
	DeliveryDiscountMinor int64                `json:"delivery_discount_minor"`
	TotalMinor            int64                `json:"total_minor"`
	Participants          []ParticipantSummary `json:"participants"`
}

func (c *GroupCart) AddItem(item CartItem, menuItem MenuItem) error {
	if c.Status == CartLocked {
		return ErrCartLocked
	}
	if item.Quantity <= 0 {
		return ErrInvalidQuantity
	}
	if item.RestaurantID != c.RestaurantID || menuItem.RestaurantID != c.RestaurantID {
		return ErrRestaurantMismatch
	}
	if !validateOptions(menuItem.OptionGroups, item.Options) {
		return ErrInvalidSelection
	}
	item.LineTotalMinor = int64(item.Quantity) * (item.UnitPriceMinor + optionDelta(item.Options))
	c.Items = append(c.Items, item)
	CalculateTotals(c, 15_000)
	return nil
}
func (c *GroupCart) RemoveItem(id string) error {
	if c.Status == CartLocked {
		return ErrCartLocked
	}
	for index, item := range c.Items {
		if item.ID == id {
			c.Items = append(c.Items[:index], c.Items[index+1:]...)
			CalculateTotals(c, 15_000)
			return nil
		}
	}
	return ErrItemNotFound
}
func (c *GroupCart) Lock() error {
	if c.Status == CartLocked {
		return ErrCartLocked
	}
	c.Status = CartLocked
	return nil
}

func CalculateTotals(c *GroupCart, baseDeliveryFeeMinor int64) {
	c.SubtotalMinor = 0
	byParticipant := map[string]*ParticipantSummary{}
	for _, item := range c.Items {
		c.SubtotalMinor += item.LineTotalMinor
		key := item.Participant.Key()
		if byParticipant[key] == nil {
			byParticipant[key] = &ParticipantSummary{Participant: item.Participant}
		}
		byParticipant[key].SubtotalMinor += item.LineTotalMinor
	}
	c.DeliveryDiscountMinor = 0
	if c.SubtotalMinor >= GroupDiscountThresholdMinor {
		c.DeliveryDiscountMinor = baseDeliveryFeeMinor / 2
	}
	c.DeliveryFeeMinor = baseDeliveryFeeMinor - c.DeliveryDiscountMinor
	c.TotalMinor = c.SubtotalMinor + c.DeliveryFeeMinor
	c.Participants = c.Participants[:0]
	keys := make([]string, 0, len(byParticipant))
	for key := range byParticipant {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		c.Participants = append(c.Participants, *byParticipant[key])
	}
	var assigned int64
	for index := range c.Participants {
		if index == len(c.Participants)-1 {
			c.Participants[index].DeliveryMinor = c.DeliveryFeeMinor - assigned
		} else if c.SubtotalMinor > 0 {
			c.Participants[index].DeliveryMinor = c.DeliveryFeeMinor * c.Participants[index].SubtotalMinor / c.SubtotalMinor
		}
		assigned += c.Participants[index].DeliveryMinor
		c.Participants[index].TotalMinor = c.Participants[index].SubtotalMinor + c.Participants[index].DeliveryMinor
	}
}
func optionDelta(options []SelectedOption) int64 {
	var total int64
	for _, option := range options {
		total += option.PriceDeltaMinor
	}
	return total
}
func validateOptions(groups []OptionGroup, selected []SelectedOption) bool {
	byGroup := map[string][]SelectedOption{}
	for _, option := range selected {
		byGroup[option.GroupID] = append(byGroup[option.GroupID], option)
	}
	for _, group := range groups {
		choices := byGroup[group.ID]
		if len(choices) < group.MinSelect || len(choices) > group.MaxSelect {
			return false
		}
		seen := map[string]bool{}
		for _, choice := range choices {
			if seen[choice.OptionID] {
				return false
			}
			seen[choice.OptionID] = true
			found := false
			for _, option := range group.Options {
				if option.ID == choice.OptionID && option.IsAvailable && option.PriceDeltaMinor == choice.PriceDeltaMinor {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	for groupID := range byGroup {
		found := false
		for _, group := range groups {
			if group.ID == groupID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
