package domain

import "errors"

var (
	ErrInvalidSearch      = errors.New("invalid restaurant search")
	ErrRestaurantNotFound = errors.New("restaurant not found")
	ErrRestaurantClosed   = errors.New("restaurant is closed")
	ErrTooFewOptions      = errors.New("too few options selected")
	ErrTooManyOptions     = errors.New("too many options selected")
	ErrDuplicateOption    = errors.New("duplicate option selected")
	ErrUnavailableOption  = errors.New("unavailable option selected")
	ErrUnknownOption      = errors.New("unknown option selected")
)

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
