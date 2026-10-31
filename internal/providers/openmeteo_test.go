package providers

import (
	"testing"
	"time"
)

func TestBestGeocodeMatchUsesRegionAbbreviation(t *testing.T) {
	results := []geocodeResult{
		{Name: "Saskatoon Mountain", Country: "Canada", CountryCode: "CA", Admin1: "Alberta"},
		{Name: "Saskatoon", Country: "Canada", CountryCode: "CA", Admin1: "Saskatchewan"},
	}
	name, qualifiers := splitLocationQuery("Saskatoon, SK, Canada")
	if name != "Saskatoon" {
		t.Fatalf("name = %q", name)
	}
	match, ok := bestGeocodeMatch(results, qualifiers)
	if !ok || match.Admin1 != "Saskatchewan" {
		t.Fatalf("got %+v, %v; want the Saskatchewan result", match, ok)
	}
	if _, ok := bestGeocodeMatch(results, []string{"TX"}); ok {
		t.Fatal("a qualifier that matches nothing should not resolve")
	}
}

func TestDominantCode(t *testing.T) {
	cases := []struct {
		name  string
		codes []int
		want  int
	}{
		{"one stray drizzle hour keeps the sky code", []int{0, 0, 51, 0, 1, 0}, 0},
		{"two rain hours win", []int{2, 61, 61, 3, 2}, 61},
		{"most severe sustained code wins", []int{61, 61, 71, 71, 3}, 71},
		{"a single thunderstorm hour counts", []int{1, 1, 95, 1}, 95},
	}
	for _, c := range cases {
		if got := dominantCode(c.codes); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestMoonPhaseAt(t *testing.T) {
	newMoon := time.Date(2024, 1, 11, 12, 0, 0, 0, time.UTC)
	fullMoon := time.Date(2024, 1, 25, 18, 0, 0, 0, time.UTC)
	if got := moonPhaseAt(newMoon).code; got != "N" {
		t.Errorf("2024-01-11 = %s, want N", got)
	}
	if got := moonPhaseAt(fullMoon).code; got != "F" {
		t.Errorf("2024-01-25 = %s, want F", got)
	}
}

// syntheticForecast is a flat forecast in America/Regina (no DST) starting at midnight the day before
// `day`, shaped like a past_days=1, forecast_days=9 response.
func syntheticForecast(t *testing.T, day time.Time) *omForecast {
	t.Helper()
	location, err := time.LoadLocation("America/Regina")
	if err != nil {
		t.Fatal(err)
	}
	f := &omForecast{Latitude: 52.13, Longitude: -106.67, Timezone: "America/Regina", location: location}
	start := time.Date(day.Year(), day.Month(), day.Day()-1, 0, 0, 0, 0, location)
	for d := range 10 {
		midnight := start.AddDate(0, 0, d)
		f.Daily.Time = append(f.Daily.Time, midnight.Unix())
		f.Daily.TemperatureMax = append(f.Daily.TemperatureMax, 10)
		f.Daily.TemperatureMin = append(f.Daily.TemperatureMin, 0)
		f.Daily.Sunrise = append(f.Daily.Sunrise, midnight.Add(7*time.Hour).Unix())
		f.Daily.Sunset = append(f.Daily.Sunset, midnight.Add(18*time.Hour).Unix())
		f.Daily.PrecipitationSum = append(f.Daily.PrecipitationSum, 0)
		f.Daily.UVIndexMax = append(f.Daily.UVIndexMax, 0)
	}
	for h := range 240 {
		hour := start.Add(time.Duration(h) * time.Hour)
		f.Hourly.Time = append(f.Hourly.Time, hour.Unix())
		f.Hourly.Temperature = append(f.Hourly.Temperature, float64(hour.Hour()))
		f.Hourly.ApparentTemperature = append(f.Hourly.ApparentTemperature, float64(hour.Hour()))
		f.Hourly.WeatherCode = append(f.Hourly.WeatherCode, 2)
		f.Hourly.IsDay = append(f.Hourly.IsDay, 1)
		f.Hourly.CloudCover = append(f.Hourly.CloudCover, 50)
		f.Hourly.PrecipitationProbability = append(f.Hourly.PrecipitationProbability, 0)
		f.Hourly.Precipitation = append(f.Hourly.Precipitation, 0)
		f.Hourly.Rain = append(f.Hourly.Rain, 0)
		f.Hourly.Showers = append(f.Hourly.Showers, 0)
		f.Hourly.Snowfall = append(f.Hourly.Snowfall, 0)
		f.Hourly.PressureMSL = append(f.Hourly.PressureMSL, 1013)
		f.Hourly.Visibility = append(f.Hourly.Visibility, 20000)
		f.Hourly.WindSpeed = append(f.Hourly.WindSpeed, 20)
		f.Hourly.WindDirection = append(f.Hourly.WindDirection, 225)
		f.Hourly.WindGusts = append(f.Hourly.WindGusts, 25)
	}
	return f
}

// editHours applies edit to every hourly index in [from, to).
func editHours(f *omForecast, from, to time.Time, edit func(i int, local time.Time)) {
	for i, unix := range f.Hourly.Time {
		local := time.Unix(unix, 0).In(f.location)
		if !local.Before(from) && local.Before(to) {
			edit(i, local)
		}
	}
}

func TestDailyDropsTodayInTheAfternoon(t *testing.T) {
	location, _ := time.LoadLocation("America/Regina")
	now := time.Date(2026, 10, 30, 16, 0, 0, 0, location) // a Friday
	b := newForecastBuilder(syntheticForecast(t, now), units("m"), now)

	daily := b.daily(8)
	parts := daily.Daypart[0]
	if len(daily.DayOfWeek) != 8 || len(parts.DaypartName) != 16 {
		t.Fatalf("got %d days and %d dayparts, want 8 and 16", len(daily.DayOfWeek), len(parts.DaypartName))
	}
	if parts.DaypartName[0] != nil {
		t.Errorf("Today should be dropped after %d:00, got %q", todayDropHour, *parts.DaypartName[0])
	}
	wantNames := []string{"Tonight", "Tomorrow", "Tomorrow night", "Sunday"}
	for i, want := range wantNames {
		if got := *parts.DaypartName[i+1]; got != want {
			t.Errorf("daypart %d = %q, want %q", i+1, got, want)
		}
	}
	// Daytime temperature is the 07:00-19:00 high (18), night is the 19:00-07:00 low (0).
	if *parts.Temperature[2] != 18 || *parts.Temperature[1] != 0 {
		t.Errorf("tomorrow high = %d, tonight low = %d; want 18 and 0", *parts.Temperature[2], *parts.Temperature[1])
	}

	intraday := b.intraday(4)
	if intraday[0].DaypartName != "Afternoon" || intraday[3].DaypartName != "Morning" {
		t.Errorf("intraday runs %s..%s, want Afternoon..Morning", intraday[0].DaypartName, intraday[3].DaypartName)
	}

	hourly := b.hourly(48)
	if hourly.ValidTimeLocal[0] != "2026-10-30T17:00:00-0600" {
		t.Errorf("hourly starts at %s, want the next hour", hourly.ValidTimeLocal[0])
	}
}

func TestShortTermNarrativeDescribesChanges(t *testing.T) {
	location, _ := time.LoadLocation("America/Regina")
	now := time.Date(2026, 10, 30, 16, 0, 0, 0, location) // Friday; Today is already dropped
	tonight := time.Date(2026, 10, 30, 19, 0, 0, 0, location)
	midnight := time.Date(2026, 10, 31, 1, 0, 0, 0, location)
	tomorrow := time.Date(2026, 10, 31, 7, 0, 0, 0, location)
	f := syntheticForecast(t, now)

	// Tonight: overcast and calm, then snow with a gusty northwest wind and wind chill after midnight.
	editHours(f, tonight, tomorrow, func(i int, local time.Time) {
		f.Hourly.CloudCover[i], f.Hourly.Temperature[i], f.Hourly.ApparentTemperature[i] = 100, -2, -6
		f.Hourly.WindSpeed[i], f.Hourly.WindDirection[i], f.Hourly.WindGusts[i] = 10, 0, 15
		if !local.Before(midnight) {
			f.Hourly.WeatherCode[i], f.Hourly.PrecipitationProbability[i], f.Hourly.Snowfall[i] = 73, 80, 0.5
			f.Hourly.WindSpeed[i], f.Hourly.WindDirection[i], f.Hourly.WindGusts[i] = 35, 315, 60
			f.Hourly.ApparentTemperature[i] = -12
		}
	})
	// Tomorrow: high early in the morning then falling, showers likely in the afternoon.
	editHours(f, tomorrow, tomorrow.Add(12*time.Hour), func(i int, local time.Time) {
		f.Hourly.CloudCover[i] = 70
		f.Hourly.Temperature[i] = 8 - float64(local.Hour()-7)
		f.Hourly.ApparentTemperature[i] = f.Hourly.Temperature[i]
		if local.Hour() >= 13 {
			f.Hourly.PrecipitationProbability[i] = 40
		}
	})
	f.Daily.UVIndexMax[2] = 2.6

	narratives := newForecastBuilder(f, units("m"), now).daily(4).Daypart[0].Narrative
	want := map[int]string{
		1: "Cloudy. Snow beginning after midnight. Snowfall amount 2 to 4 cm. Wind becoming northwest 35 km/h gusting to 60 after midnight. Low minus 2. Wind chill minus 12 overnight.",
		2: "Mainly cloudy. 40 percent chance of showers in the afternoon. Wind southwest 20 km/h. High 8 in the morning. Temperature falling to minus 3 in the afternoon. UV index 3 or moderate.",
		// Past the short term: the one-line summary.
		6: "Partly cloudy. High 18C. Winds SW at 20 km/h.",
	}
	for index, text := range want {
		if got := *narratives[index]; got != text {
			t.Errorf("daypart %d:\n got  %s\n want %s", index, got, text)
		}
	}
}
