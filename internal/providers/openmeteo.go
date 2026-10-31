package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // forecasts are rendered in the location's zone; embed tzdata so portable Windows builds have it
)

const (
	openMeteoForecastURL   = "https://api.open-meteo.com/v1/forecast"
	openMeteoGeocodingURL  = "https://geocoding-api.open-meteo.com/v1/search"
	openMeteoAirQualityURL = "https://air-quality-api.open-meteo.com/v1/air-quality"
)

// OpenMeteoClient serves weather from the keyless Open-Meteo APIs, reshaped into the TWC payload layout
// so the frontend needs no provider-specific code. Open-Meteo has no alerts, pollen outside Europe, or
// cloud ceiling, so those come back empty (no alerts, null ceiling).
type OpenMeteoClient struct {
	httpClient      *http.Client
	units           units
	persistentCache LocaleStore
	cache           TTLStore
	logf            func(format string, args ...any)
}

// NewOpenMeteoClient takes the TWC units code from serverConfig.units ("e", "m", "h" or "s").
func NewOpenMeteoClient(httpClient *http.Client, unitsCode string, persistentCache LocaleStore, cache TTLStore, logf func(format string, args ...any)) *OpenMeteoClient {
	return &OpenMeteoClient{
		httpClient:      httpClient,
		units:           units(unitsCode),
		persistentCache: persistentCache,
		cache:           cache,
		logf:            logf,
	}
}

func (o *OpenMeteoClient) LoadLocaleData(ctx context.Context, location string) (LocaleData, error) {
	// Prefixed so a locale cached by TWC (which carries a postalKey) never collides with one from here.
	cacheKey := "om-locale-" + location
	if hit, ok := o.persistentCache.Get(cacheKey); ok {
		return hit, nil
	}
	name, qualifiers := splitLocationQuery(location)
	query := url.Values{"name": {name}, "count": {"10"}, "language": {"en"}, "format": {"json"}}
	var payload struct {
		Results []geocodeResult `json:"results"`
	}
	if err := o.getJSON(ctx, openMeteoGeocodingURL+"?"+query.Encode(), &payload); err != nil {
		return LocaleData{}, err
	}
	match, ok := bestGeocodeMatch(payload.Results, qualifiers)
	if !ok {
		return LocaleData{}, fmt.Errorf("open-meteo: no location matches %q", location)
	}
	result := LocaleData{
		LocaleName:    match.Name,
		AdminDistrict: match.Admin1,
		Country:       match.Country,
		CountryCode:   match.CountryCode,
		Lat:           match.Latitude,
		Lon:           match.Longitude,
	}
	o.logf(`Fetched locale %s, %s, %s`, result.LocaleName, result.AdminDistrict, result.CountryCode)
	_ = o.persistentCache.Set(cacheKey, result)
	return result, nil
}

func (o *OpenMeteoClient) LoadWeatherData(ctx context.Context, locale LocaleData, locType string) (CachedWeatherData, error) {
	forecast, err := o.loadForecast(ctx, locale)
	if err != nil {
		return nil, err
	}
	b := newForecastBuilder(forecast, o.units, time.Now())
	switch locType {
	case "primary", "ldl":
		airQuality, err := o.loadAirQuality(ctx, locale)
		if err != nil {
			return nil, err
		}
		return CachedWeatherData{
			"v3-wx-observations-current": b.observation(),
			"v3-wx-forecast-daily-7day":  b.daily(8),
			"v3-wx-forecast-hourly-2day": b.hourly(48),
			"v2fcstintraday3":            map[string]any{"forecasts": b.intraday(4)},
			"v3-wx-globalAirQuality":     map[string]any{"globalairquality": airQuality.toTWC()},
		}, nil
	case "secondary", "national":
		return CachedWeatherData{
			"v3-wx-observations-current": b.observation(),
			"v3-wx-forecast-daily-3day":  b.daily(4),
		}, nil
	default:
		return nil, fmt.Errorf("open-meteo: unknown locType %q", locType)
	}
}

// FetchAlerts always reports no alerts: Open-Meteo does not publish them.
func (o *OpenMeteoClient) FetchAlerts(context.Context, LocaleData) (map[string]any, int, error) {
	return nil, http.StatusNoContent, nil
}

// loadForecast fetches (or reuses) the raw forecast for a location. Every locType is built from the
// same response, so it is cached per coordinate rather than per locType.
func (o *OpenMeteoClient) loadForecast(ctx context.Context, locale LocaleData) (*omForecast, error) {
	cacheKey := "om-forecast-" + locale.geocode()
	if hit, ok := o.cache.Get(cacheKey); ok {
		return hit.(*omForecast), nil
	}
	query := coordinateQuery(locale)
	query.Set("current", omCurrentFields)
	query.Set("hourly", omHourlyFields)
	query.Set("daily", omDailyFields)
	query.Set("timezone", "auto")
	query.Set("timeformat", "unixtime")
	query.Set("past_days", "1")     // the last 24 hours feed temperature change, pressure trend and precip
	query.Set("forecast_days", "9") // 8 forecast days plus the hours that close out the 8th night
	forecast := &omForecast{}
	if err := o.getJSON(ctx, openMeteoForecastURL+"?"+query.Encode(), forecast); err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(forecast.Timezone)
	if err != nil {
		return nil, fmt.Errorf("open-meteo: time zone %q: %w", forecast.Timezone, err)
	}
	forecast.location = location
	o.cache.Set(cacheKey, forecast)
	o.logf("Fetched forecast for %s, %s", locale.LocaleName, locale.AdminDistrict)
	return forecast, nil
}

func (o *OpenMeteoClient) loadAirQuality(ctx context.Context, locale LocaleData) (omAirQuality, error) {
	cacheKey := "om-aq-" + locale.geocode()
	if hit, ok := o.cache.Get(cacheKey); ok {
		return hit.(omAirQuality), nil
	}
	query := coordinateQuery(locale)
	query.Set("current", omAirQualityFields())
	query.Set("timeformat", "unixtime") // keeps every value in Current numeric
	var airQuality omAirQuality
	if err := o.getJSON(ctx, openMeteoAirQualityURL+"?"+query.Encode(), &airQuality); err != nil {
		return omAirQuality{}, err
	}
	o.cache.Set(cacheKey, airQuality)
	return airQuality, nil
}

func (o *OpenMeteoClient) getJSON(ctx context.Context, target string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	request.Header = weatherAPIHeaders()
	response, err := o.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(response.Body).Decode(&failure)
		return fmt.Errorf("open-meteo %s: %s %s", request.URL.Host, response.Status, failure.Reason)
	}
	return json.NewDecoder(response.Body).Decode(out)
}

func coordinateQuery(locale LocaleData) url.Values {
	return url.Values{
		"latitude":  {strconv.FormatFloat(locale.Lat, 'f', -1, 64)},
		"longitude": {strconv.FormatFloat(locale.Lon, 'f', -1, 64)},
	}
}

func joinFields(fields ...string) string {
	return strings.Join(fields, ",")
}
