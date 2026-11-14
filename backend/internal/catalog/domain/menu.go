package domain

type Menu struct {
	RestaurantID   RestaurantID   `json:"restaurant_id"`
	RestaurantName string         `json:"restaurant_name"`
	Categories     []MenuCategory `json:"categories"`
}

type MenuCategory struct {
	ID    CategoryID `json:"id"`
	Name  string     `json:"name"`
	Items []MenuItem `json:"items"`
}

type MenuItem struct {
	ID           MenuItemID    `json:"id"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	ImageURL     string        `json:"image_url,omitempty"`
	PriceMinor   int64         `json:"price_minor"`
	IsAvailable  bool          `json:"is_available"`
	OptionGroups []OptionGroup `json:"option_groups,omitempty"`
}

type OptionGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	MinSelect int      `json:"min_select"`
	MaxSelect int      `json:"max_select"`
	Options   []Option `json:"options"`
}

type Option struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PriceDeltaMinor int64  `json:"price_delta"`
	IsAvailable     bool   `json:"is_available"`
}

func ValidateOptionSelection(group OptionGroup, selected []string) error {
	if len(selected) < group.MinSelect {
		return ErrTooFewOptions
	}
	if len(selected) > group.MaxSelect {
		return ErrTooManyOptions
	}
	known := make(map[string]Option, len(group.Options))
	for _, option := range group.Options {
		known[option.ID] = option
	}
	seen := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		if _, exists := seen[id]; exists {
			return ErrDuplicateOption
		}
		seen[id] = struct{}{}
		option, exists := known[id]
		if !exists {
			return ErrUnknownOption
		}
		if !option.IsAvailable {
			return ErrUnavailableOption
		}
	}
	return nil
}
