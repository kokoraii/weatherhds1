package providers

import "context"

// Provider resolves location queries and serves weather payloads in the TWC-shaped JSON
// ("v3-wx-observations-current", "v3-wx-forecast-daily-7day", ...) that the frontend reads.
type Provider interface {
	LoadLocaleData(ctx context.Context, location string) (LocaleData, error)
	// LoadWeatherData returns the payload for one locType: "primary"/"ldl" get the full bundle,
	// "secondary"/"national" get current conditions and a short daily forecast.
	LoadWeatherData(ctx context.Context, locale LocaleData, locType string) (CachedWeatherData, error)
	// FetchAlerts returns the alert bundle and http.StatusOK, or http.StatusNoContent when none are active.
	FetchAlerts(ctx context.Context, locale LocaleData) (map[string]any, int, error)
}
