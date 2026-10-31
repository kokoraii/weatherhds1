package providers

import "fmt"

type LocaleData struct {
	LocaleName    string  `json:"localeName"`
	AdminDistrict string  `json:"adminDistrict"`
	Country       string  `json:"country"`
	CountryCode   string  `json:"countryCode"`
	Lat           float64 `json:"lat"`
	Lon           float64 `json:"lon"`
	PostalKey     string  `json:"postalKey"`
}

// geocode formats the coordinates as the "lat,lon" pair TWC endpoints expect.
func (l LocaleData) geocode() string {
	return fmt.Sprintf("%v,%v", l.Lat, l.Lon)
}

type CachedWeatherData map[string]any

type LocaleStore interface {
	Get(key string) (LocaleData, bool)
	Set(key string, value LocaleData) error
}

type TTLStore interface {
	Get(key string) (any, bool)
	Set(key string, value any)
}
