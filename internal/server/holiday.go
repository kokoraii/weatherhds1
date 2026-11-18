package server

import (
	"context"
	"time"

	"weatherhds/internal/events"
)

// holidaySync tells the pages today's date in the station's time zone (health holidaySync), as each page
// connects and again at every local midnight. The page looks it up in holidayMapping.
func holidaySync(now time.Time, zone *time.Location) events.Message {
	return events.NewMessage(events.TypeHealth, "holidaySync", map[string]string{"date": now.In(zone).Format("01-02")})
}

// runHolidaySync sends holidaySync to every page at each local midnight until ctx ends.
func runHolidaySync(ctx context.Context, hub *events.Hub, zone *time.Location) {
	for {
		now := time.Now().In(zone)
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, zone)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(midnight)):
			hub.Broadcast(holidaySync(time.Now(), zone))
		}
	}
}

// stationZone is config.systemTimeZone, or the server's own zone if that's unset or unknown.
func stationZone(name string) *time.Location {
	if zone, err := time.LoadLocation(name); err == nil && name != "" {
		return zone
	}
	return time.Local
}
