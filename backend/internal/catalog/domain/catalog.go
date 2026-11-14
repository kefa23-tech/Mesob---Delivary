package domain

import "errors"

var ErrInvalidSearch = errors.New("invalid restaurant search")

type RestaurantID string
type MenuItemID string
type CategoryID string

type Restaurant struct {
	ID                    RestaurantID `json:"id"`
	Name                  string       `json:"name"`
	Lat                   float64      `json:"lat"`
	Lng                   float64      `json:"lng"`
	DistanceMeters        float64      `json:"distance_meters"`
	DeliveryRadiusKm      float64      `json:"delivery_radius_km"`
	IsOpen                bool         `json:"is_open"`
	EcoPackagingCertified bool         `json:"eco_packaging_certified"`
	Cuisines              []string     `json:"cuisines,omitempty"`
}

type Category struct {
	ID   CategoryID
	Name string
}
type MenuItem struct {
	ID         MenuItemID
	Restaurant RestaurantID
	Name       string
	PriceCents int64
	Available  bool
}

type SearchFilter struct {
	Lat          float64
	Lng          float64
	Cuisine      string
	Query        string
	EcoPackaging *bool
	Cursor       Cursor
	Limit        int
}
type Cursor struct {
	DistanceMeters float64      `json:"distance_meters"`
	RestaurantID   RestaurantID `json:"restaurant_id"`
}
type SearchResult struct {
	Restaurants []Restaurant `json:"restaurants"`
	NextCursor  string       `json:"next_cursor,omitempty"`
}

func (f SearchFilter) Valid() bool {
	return f.Lat >= -90 && f.Lat <= 90 && f.Lng >= -180 && f.Lng <= 180 && f.Limit > 0 && f.Limit <= 50
}
