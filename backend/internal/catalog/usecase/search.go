package usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/mesob-delivery/backend/internal/catalog/domain"
	"net/url"
	"strconv"
	"strings"
)

const defaultPageSize = 20

type Service struct {
	repository RestaurantRepository
	cache      RestaurantCache
}

func NewService(repository RestaurantRepository, cache RestaurantCache) *Service {
	return &Service{repository: repository, cache: cache}
}

func (s *Service) Search(ctx context.Context, filter domain.SearchFilter) (domain.SearchResult, error) {
	if filter.Limit == 0 {
		filter.Limit = defaultPageSize
	}
	if !filter.Valid() {
		return domain.SearchResult{}, domain.ErrInvalidSearch
	}
	key := cacheKey(filter)
	if s.cache != nil {
		if result, found, err := s.cache.Get(ctx, key); err == nil && found {
			return result, nil
		}
	}
	result, err := s.repository.Search(ctx, filter)
	if err != nil {
		return domain.SearchResult{}, err
	}
	if len(result.Restaurants) > filter.Limit {
		result.Restaurants = result.Restaurants[:filter.Limit]
		last := result.Restaurants[len(result.Restaurants)-1]
		result.NextCursor = EncodeCursor(domain.Cursor{DistanceMeters: last.DistanceMeters, RestaurantID: last.ID})
	}
	if s.cache != nil {
		_ = s.cache.Set(ctx, key, result)
	}
	return result, nil
}

func EncodeCursor(cursor domain.Cursor) string {
	value, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(value)
}
func DecodeCursor(value string) (domain.Cursor, error) {
	if value == "" {
		return domain.Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return domain.Cursor{}, domain.ErrInvalidSearch
	}
	var cursor domain.Cursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.RestaurantID == "" {
		return domain.Cursor{}, domain.ErrInvalidSearch
	}
	return cursor, nil
}
func cacheKey(filter domain.SearchFilter) string {
	eco := ""
	if filter.EcoPackaging != nil {
		eco = strconv.FormatBool(*filter.EcoPackaging)
	}
	return "restaurants:v1:" + url.QueryEscape(strings.Join([]string{fmt.Sprintf("%.6f", filter.Lat), fmt.Sprintf("%.6f", filter.Lng), strings.ToLower(strings.TrimSpace(filter.Cuisine)), strings.ToLower(strings.TrimSpace(filter.Query)), eco, fmt.Sprintf("%.6f", filter.Cursor.DistanceMeters), string(filter.Cursor.RestaurantID), strconv.Itoa(filter.Limit)}, "|"))
}
