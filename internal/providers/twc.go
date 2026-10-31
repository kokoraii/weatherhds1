package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type TWCClient struct {
	httpClient      *http.Client
	apiKey          string
	systemLocale    string
	units           string
	persistentCache LocaleStore
	cache           TTLStore
	mainAggCommon   string
	mainV1AggCommon string
	minorAggCommon  string
	logf            func(format string, args ...any)
}

func NewTWCClient(httpClient *http.Client, apiKey string, systemLocale string, units string, persistentCache LocaleStore, cache TTLStore, logf func(format string, args ...any)) *TWCClient {
	return &TWCClient{
		httpClient:      httpClient,
		apiKey:          apiKey,
		systemLocale:    systemLocale,
		units:           units,
		persistentCache: persistentCache,
		cache:           cache,
		mainAggCommon:   "v3-wx-observations-current;v3-wx-forecast-daily-7day;v3-wx-globalAirQuality;v3-wx-forecast-hourly-2day",
		mainV1AggCommon: "v2fcstintraday3;v2fcstwwir",
		minorAggCommon:  "v3-wx-observations-current;v3-wx-forecast-daily-3day",
		logf:            logf,
	}
}

func (t *TWCClient) LoadLocaleData(ctx context.Context, location string) (LocaleData, error) {
	cacheKey := "locale-" + location
	if hit, ok := t.persistentCache.Get(cacheKey); ok {
		t.logf(`Cache hit for client query %q`, location)
		return hit, nil
	}
	fetchURL := fmt.Sprintf("https://api.weather.com/v3/location/search?query=%s&language=%s&format=json&apiKey=%s", url.QueryEscape(location), url.QueryEscape(t.systemLocale), url.QueryEscape(t.apiKey))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return LocaleData{}, err
	}
	request.Header = weatherAPIHeaders()
	response, err := t.httpClient.Do(request)
	if err != nil {
		return LocaleData{}, err
	}
	defer response.Body.Close()
	var payload struct {
		Location struct {
			City          []string  `json:"city"`
			AdminDistrict []string  `json:"adminDistrict"`
			Country       []string  `json:"country"`
			CountryCode   []string  `json:"countryCode"`
			Latitude      []float64 `json:"latitude"`
			Longitude     []float64 `json:"longitude"`
			PostalKey     []string  `json:"postalKey"`
		} `json:"location"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return LocaleData{}, err
	}
	result := LocaleData{
		LocaleName:    firstOrEmpty(payload.Location.City),
		AdminDistrict: firstOrEmpty(payload.Location.AdminDistrict),
		Country:       firstOrEmpty(payload.Location.Country),
		CountryCode:   firstOrEmpty(payload.Location.CountryCode),
		Lat:           firstOrZero(payload.Location.Latitude),
		Lon:           firstOrZero(payload.Location.Longitude),
		PostalKey:     firstOrEmpty(payload.Location.PostalKey),
	}
	t.logf(`Fetched locale %s, %s, %s`, result.LocaleName, result.AdminDistrict, result.CountryCode)
	_ = t.persistentCache.Set(cacheKey, result)
	return result, nil
}

func (t *TWCClient) LoadWeatherData(ctx context.Context, locale LocaleData, locType string) (CachedWeatherData, error) {
	postalKey := locale.PostalKey
	geocode := locale.geocode()
	baseKey := fmt.Sprintf("wxData-%s:%s", postalKey, geocode)
	cacheKey := fmt.Sprintf("%s:%s", baseKey, locType)
	if hit, ok := t.cache.Get(cacheKey); ok {
		t.logf("Returned cache key: %s", cacheKey)
		return hit.(CachedWeatherData), nil
	}
	if locType == "secondary" || locType == "national" {
		if primary, ok := t.cache.Get(baseKey + ":primary"); ok {
			return primary.(CachedWeatherData), nil
		}
		if ldl, ok := t.cache.Get(baseKey + ":ldl"); ok {
			return ldl.(CachedWeatherData), nil
		}
	}
	var data CachedWeatherData
	if locType == "primary" || locType == "ldl" {
		aggOneURL := fmt.Sprintf("https://api.weather.com/v3/aggcommon/%s?postalKey=%s&language=%s&scale=EPA&units=%s&format=json&apiKey=%s", t.mainAggCommon, postalKey, t.systemLocale, t.units, t.apiKey)
		aggTwoURL := fmt.Sprintf("https://api.weather.com/v2/aggcommon/%s?geocode=%s&language=%s&units=%s&format=json&apiKey=%s", t.mainV1AggCommon, geocode, t.systemLocale, t.units, t.apiKey)
		pollenURL := fmt.Sprintf("https://api.weather.com/v2/indices/pollen/daypart/15day?geocode=%s&language=%s&format=json&apiKey=%s", geocode, t.systemLocale, t.apiKey)
		aggOne, err := t.fetchJSON(ctx, aggOneURL)
		if err != nil {
			return nil, err
		}
		aggTwo, err := t.fetchJSON(ctx, aggTwoURL)
		if err != nil {
			return nil, err
		}
		pollen, err := t.fetchAny(ctx, pollenURL)
		if err != nil {
			return nil, err
		}
		merged := map[string]any{}
		for key, value := range aggOne {
			merged[key] = value
		}
		for key, value := range aggTwo {
			merged[key] = value
		}
		merged["pollenData"] = pollen
		data = merged
		t.logf("Fetched and cached: %s", cacheKey)
	}
	if locType == "secondary" || locType == "national" {
		requestURL := fmt.Sprintf("https://api.weather.com/v3/aggcommon/%s?postalKey=%s&language=%s&units=%s&format=json&apiKey=%s", t.minorAggCommon, postalKey, t.systemLocale, t.units, t.apiKey)
		minor, err := t.fetchJSON(ctx, requestURL)
		if err != nil {
			return nil, err
		}
		data = minor
	}
	t.cache.Set(cacheKey, data)
	return data, nil
}

func (t *TWCClient) FetchAlerts(ctx context.Context, locale LocaleData) (map[string]any, int, error) {
	geocode := locale.geocode()
	cacheKey := fmt.Sprintf("alert-%s-none", geocode)
	if hit, ok := t.cache.Get(cacheKey); ok {
		return hit.(map[string]any), http.StatusOK, nil
	}
	requestURL := fmt.Sprintf("https://api.weather.com/v3/alerts/headlines?geocode=%s&language=%s&format=json&apiKey=%s", geocode, t.systemLocale, t.apiKey)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header = weatherAPIHeaders()
	response, err := t.httpClient.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, http.StatusNoContent, nil
	}
	var headline map[string]any
	if err := json.NewDecoder(response.Body).Decode(&headline); err != nil {
		return nil, 0, err
	}
	bundle := map[string]any{"headline": headline, "detail": nil}
	alerts, _ := headline["alerts"].([]any)
	if len(alerts) > 0 {
		firstAlert, _ := alerts[0].(map[string]any)
		detailKey, _ := firstAlert["detailKey"].(string)
		if detailKey != "" {
			detailURL := fmt.Sprintf("https://api.weather.com/v3/alerts/detail?alertId=%s&language=%s&format=json&apiKey=%s", url.QueryEscape(detailKey), t.systemLocale, t.apiKey)
			detail, err := t.fetchAny(ctx, detailURL)
			if err != nil {
				return nil, 0, err
			}
			bundle["detail"] = detail
		}
	}
	t.cache.Set(cacheKey, bundle)
	return bundle, http.StatusOK, nil
}

func (t *TWCClient) fetchJSON(ctx context.Context, target string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header = weatherAPIHeaders()
	response, err := t.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (t *TWCClient) fetchAny(ctx context.Context, target string) (any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header = weatherAPIHeaders()
	response, err := t.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var payload any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func weatherAPIHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", "WeatherHDS/Go")
	headers.Set("Connection", "keep-alive")
	return headers
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstOrZero(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[0]
}
