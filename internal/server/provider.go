package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"weatherhds/internal/providers"
)

// newProvider builds the weather provider named by serverConfig.dataProvider ("twc" or "openmeteo").
// Left unset, it picks TWC when TWC_API_KEY is present and the keyless Open-Meteo otherwise.
func newProvider(name string, twcAPIKey string, units string, client *http.Client, localeCache providers.LocaleStore, ttlCache providers.TTLStore, logger *slog.Logger) (providers.Provider, error) {
	if name == "" {
		name = "openmeteo"
		if twcAPIKey != "" {
			name = "twc"
		}
	}
	switch name {
	case "twc":
		if twcAPIKey == "" {
			return nil, errors.New(`dataProvider "twc" needs TWC_API_KEY; set it, or use dataProvider "openmeteo"`)
		}
		logInfo(logger, "server", "Weather data provider: The Weather Company")
		return providers.NewTWCClient(client, twcAPIKey, "en-US", units, localeCache, ttlCache, func(format string, args ...any) {
			if strings.HasPrefix(format, "Cache hit") || strings.HasPrefix(format, "Returned cache key") {
				return
			}
			logInfo(logger, "providers/twc", format, args...)
		}), nil
	case "openmeteo":
		logInfo(logger, "server", "Weather data provider: Open-Meteo (no alerts)")
		if twcAPIKey == "" {
			logInfo(logger, "server", "No TWC_API_KEY set, so the radar will not load")
		}
		return providers.NewOpenMeteoClient(client, units, localeCache, ttlCache, func(format string, args ...any) {
			logInfo(logger, "providers/openmeteo", format, args...)
		}), nil
	default:
		return nil, fmt.Errorf("unknown dataProvider %q (expected \"twc\" or \"openmeteo\")", name)
	}
}
