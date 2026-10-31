package providers

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

const (
	// todayDropHour is the local hour after which the "Today" daypart is dropped, as TWC does.
	todayDropHour = 15
	// gustReportKmh is how far gusts must exceed the sustained wind before they are reported.
	gustReportKmh = 15
	// lightWindKmh is the mean wind below which a narrative says "Winds light."
	lightWindKmh = 10
)

// forecastBuilder renders one Open-Meteo forecast into the TWC-shaped products, in the location's zone.
type forecastBuilder struct {
	f     *omForecast
	units units
	now   time.Time
}

func newForecastBuilder(f *omForecast, u units, now time.Time) forecastBuilder {
	return forecastBuilder{f: f, units: u, now: now.In(f.location)}
}

func (b forecastBuilder) observation() twcObservation {
	c, h, d := b.f.Current, b.f.Hourly, b.f.Daily
	isDay := c.IsDay == 1
	icon, phrase := conditionFor(c.WeatherCode).at(isDay)
	nowHour := min(b.hourIndex(b.now), len(h.Time)-1)
	dayAgo := nowHour - 24 // past_days=1 guarantees a full day of history
	trendCode, trend := pressureTendency(c.PressureMSL - h.PressureMSL[nowHour-3])
	today := b.dayIndex(b.now)
	var gust *int
	if c.WindGusts-c.WindSpeed >= gustReportKmh {
		value := b.units.wind(c.WindGusts)
		gust = &value
	}
	var precip24 float64
	for _, mm := range h.Precipitation[dayAgo+1 : nowHour+1] {
		precip24 += mm
	}
	return twcObservation{
		ValidTimeLocal:          b.stamp(c.Time),
		DayOfWeek:               b.now.Weekday().String(),
		DayOrNight:              dayOrNight(isDay),
		IconCode:                icon,
		WxPhraseLong:            phrase.long,
		WxPhraseMedium:          phrase.medium,
		WxPhraseShort:           phrase.short,
		Temperature:             b.units.temp(c.Temperature),
		TemperatureFeelsLike:    b.units.temp(c.ApparentTemperature),
		TemperatureDewPoint:     b.units.temp(c.DewPoint),
		TemperatureChange24Hour: b.units.temp(c.Temperature) - b.units.temp(h.Temperature[dayAgo]),
		TemperatureMax24Hour:    b.units.temp(slices.Max(h.Temperature[dayAgo : nowHour+1])),
		TemperatureMin24Hour:    b.units.temp(slices.Min(h.Temperature[dayAgo : nowHour+1])),
		RelativeHumidity:        int(math.Round(c.RelativeHumidity)),
		WindSpeed:               b.units.wind(c.WindSpeed),
		WindDirection:           int(math.Round(c.WindDirection)),
		WindDirectionCardinal:   cardinal(c.WindDirection),
		WindGust:                gust,
		PressureAltimeter:       b.units.pressure(c.PressureMSL),
		PressureTendencyCode:    trendCode,
		PressureTendencyTrend:   trend,
		Visibility:              b.units.distance(c.Visibility),
		UVIndex:                 int(math.Round(c.UVIndex)),
		UVDescription:           uvDescription(c.UVIndex),
		Precip24Hour:            b.units.precip(precip24),
		SunriseTimeLocal:        b.stamp(d.Sunrise[today]),
		SunsetTimeLocal:         b.stamp(d.Sunset[today]),
	}
}

// daily builds `days` calendar days starting today, each with a day and a night daypart.
func (b forecastBuilder) daily(days int) twcDailyForecast {
	d := b.f.Daily
	first := b.dayIndex(b.now)
	var out twcDailyForecast
	parts := make([]*daypart, 0, days*2)
	detailed := 0 // dayparts given a detailed short-term narrative so far
	for offset := range days {
		i := first + offset
		date := b.localTime(d.Time[i])
		moonrise, moonset := moonTimes(date, b.f.Latitude, b.f.Longitude)
		phase := moonPhaseAt(date.Add(12 * time.Hour))
		out.DayOfWeek = append(out.DayOfWeek, date.Weekday().String())
		out.ValidTimeLocal = append(out.ValidTimeLocal, b.stamp(d.Time[i]))
		out.CalendarDayTemperatureMax = append(out.CalendarDayTemperatureMax, b.units.temp(d.TemperatureMax[i]))
		out.CalendarDayTemperatureMin = append(out.CalendarDayTemperatureMin, b.units.temp(d.TemperatureMin[i]))
		out.Qpf = append(out.Qpf, b.units.precip(d.PrecipitationSum[i]))
		out.SunriseTimeLocal = append(out.SunriseTimeLocal, b.stamp(d.Sunrise[i]))
		out.SunsetTimeLocal = append(out.SunsetTimeLocal, b.stamp(d.Sunset[i]))
		out.MoonriseTimeLocal = append(out.MoonriseTimeLocal, optionalStamp(moonrise))
		out.MoonsetTimeLocal = append(out.MoonsetTimeLocal, optionalStamp(moonset))
		out.MoonPhase = append(out.MoonPhase, phase.name)
		out.MoonPhaseCode = append(out.MoonPhaseCode, phase.code)

		var day *daypart
		if offset > 0 || b.now.Hour() < todayDropHour {
			day = b.daypart(i, offset, true, detailed < shortTermParts)
			detailed++
		}
		night := b.daypart(i, offset, false, detailed < shortTermParts)
		detailed++
		parts = append(parts, day, night)
	}
	out.Daypart = []twcDaypartColumns{daypartColumns(parts)}
	return out
}

// hourly builds `count` hours starting with the next full hour.
func (b forecastBuilder) hourly(count int) twcHourlyForecast {
	h := b.f.Hourly
	first := b.hourIndex(b.now) + 1
	var out twcHourlyForecast
	for i := first; i < first+count; i++ {
		isDay := h.IsDay[i] == 1
		icon, phrase := conditionFor(h.WeatherCode[i]).at(isDay)
		out.ValidTimeLocal = append(out.ValidTimeLocal, b.stamp(h.Time[i]))
		out.DayOrNight = append(out.DayOrNight, dayOrNight(isDay))
		out.IconCode = append(out.IconCode, icon)
		out.WxPhraseLong = append(out.WxPhraseLong, phrase.long)
		out.WxPhraseShort = append(out.WxPhraseShort, phrase.short)
		out.Temperature = append(out.Temperature, b.units.temp(h.Temperature[i]))
		out.PrecipChance = append(out.PrecipChance, int(math.Round(h.PrecipitationProbability[i])))
		out.WindSpeed = append(out.WindSpeed, b.units.wind(h.WindSpeed[i]))
		out.WindDirectionCardinal = append(out.WindDirectionCardinal, cardinal(h.WindDirection[i]))
	}
	return out
}

// intradayPeriods are TWC's six-hour intraday blocks, in order through a day.
var intradayPeriods = [...]struct {
	startHour int
	name      string
	daytime   bool
}{
	{1, "Overnight", false},
	{7, "Morning", true},
	{13, "Afternoon", true},
	{19, "Evening", false},
}

// intraday builds `count` consecutive six-hour periods starting with the one in progress.
func (b forecastBuilder) intraday(count int) []twcIntradayPeriod {
	year, month, day := b.now.Date()
	period := len(intradayPeriods) - 1
	if b.now.Hour() < intradayPeriods[0].startHour {
		day-- // between midnight and 01:00 the previous evening is still running
	} else {
		for i, p := range intradayPeriods {
			if b.now.Hour() >= p.startHour {
				period = i
			}
		}
	}
	out := make([]twcIntradayPeriod, 0, count)
	for range count {
		p := intradayPeriods[period]
		start := time.Date(year, month, day, p.startHour, 0, 0, 0, b.f.location)
		summary := b.summarize(start, start.Add(6*time.Hour), p.daytime)
		icon, phrase := conditionFor(summary.code).at(p.daytime)
		out = append(out, twcIntradayPeriod{
			DaypartName:    p.name,
			FcstValidLocal: start.Format(twcTimeLayout),
			Phrase22Char:   phrase.medium,
			Temp:           b.units.temp(summary.temp),
			IconCode:       icon,
		})
		if period++; period == len(intradayPeriods) {
			period = 0
			day++
		}
	}
	return out
}

// daypart is one TWC daypart (07:00-19:00 day, or 19:00-07:00 night) in display units. A daypart
// already under way covers only its remaining hours.
type daypart struct {
	name         string
	dayOrNight   string
	iconCode     int
	phraseLong   string
	phraseShort  string
	temperature  int
	precipChance int
	windSpeed    int
	windCardinal string
	narrative    string
}

// detailed selects the short-term narrative over the one-line summary.
func (b forecastBuilder) daypart(dayIndex int, offset int, daytime bool, detailed bool) *daypart {
	date := b.localTime(b.f.Daily.Time[dayIndex])
	year, month, day := date.Date()
	startHour := 7
	if !daytime {
		startHour = 19
	}
	start := time.Date(year, month, day, startHour, 0, 0, 0, b.f.location)
	end := time.Date(year, month, day, startHour+12, 0, 0, 0, b.f.location)
	from := start
	if b.now.After(start) {
		from = b.now
	}
	summary := b.summarize(from, end, daytime)
	icon, phrase := conditionFor(summary.code).at(daytime)
	p := &daypart{
		name:         daypartName(date, offset, daytime),
		dayOrNight:   dayOrNight(daytime),
		iconCode:     icon,
		phraseLong:   phrase.long,
		phraseShort:  phrase.short,
		temperature:  b.units.temp(summary.temp),
		precipChance: int(math.Round(summary.precipChance)),
		windSpeed:    b.units.wind(summary.windKmh),
		windCardinal: cardinal(summary.windDir),
	}
	if detailed {
		p.narrative = b.newNarrativePeriod(start, daytime, offset == 0, b.f.Daily.UVIndexMax[dayIndex]).text()
	} else {
		p.narrative = b.narrative(p, summary.windKmh, daytime)
	}
	return p
}

// narrative writes the one-line summary used past the short term, e.g. "Partly cloudy. High 12C. Winds SW at 15 km/h."
func (b forecastBuilder) narrative(p *daypart, windKmh float64, daytime bool) string {
	var sb strings.Builder
	label := "Low"
	if daytime {
		label = "High"
	}
	fmt.Fprintf(&sb, "%s. %s %d%s.", sentenceCase(p.phraseLong), label, p.temperature, b.units.tempLetter())
	if windKmh < lightWindKmh {
		sb.WriteString(" Winds light.")
	} else {
		fmt.Fprintf(&sb, " Winds %s at %d %s.", p.windCardinal, p.windSpeed, b.units.windLabel())
	}
	if p.precipChance >= 20 {
		fmt.Fprintf(&sb, " Chance of precip %d%%.", p.precipChance)
	}
	return sb.String()
}

// periodSummary condenses the hourly data in a period, still in Open-Meteo's metric units.
type periodSummary struct {
	code         int
	temp         float64 // high for daytime periods, low for night ones
	precipChance float64
	windKmh      float64
	windDir      float64
}

func (b forecastBuilder) summarize(start, end time.Time, daytime bool) periodSummary {
	h := b.f.Hourly
	from, to := b.hourIndex(start), b.hourIndex(end)
	summary := periodSummary{
		code:         dominantCode(h.WeatherCode[from:to]),
		precipChance: slices.Max(h.PrecipitationProbability[from:to]),
	}
	if daytime {
		summary.temp = slices.Max(h.Temperature[from:to])
	} else {
		summary.temp = slices.Min(h.Temperature[from:to])
	}
	summary.windKmh, summary.windDir = meanWind(h.WindSpeed[from:to], h.WindDirection[from:to])
	return summary
}

// hourIndex returns the index of the hourly slot containing t, clamped to [0, len] so it can bound a slice.
func (b forecastBuilder) hourIndex(t time.Time) int {
	times := b.f.Hourly.Time
	return max(0, min(int((t.Unix()-times[0])/3600), len(times)))
}

// dayIndex returns the index of the daily entry for the calendar day containing t.
func (b forecastBuilder) dayIndex(t time.Time) int {
	index := 0
	for i, midnight := range b.f.Daily.Time {
		if midnight <= t.Unix() {
			index = i
		}
	}
	return index
}

func (b forecastBuilder) localTime(unix int64) time.Time {
	return time.Unix(unix, 0).In(b.f.location)
}

func (b forecastBuilder) stamp(unix int64) string {
	return b.localTime(unix).Format(twcTimeLayout)
}

func optionalStamp(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	formatted := t.Format(twcTimeLayout)
	return &formatted
}

func daypartName(date time.Time, offset int, daytime bool) string {
	name := date.Weekday().String()
	switch offset {
	case 0:
		if daytime {
			return "Today"
		}
		return "Tonight"
	case 1:
		name = "Tomorrow"
	}
	if daytime {
		return name
	}
	return name + " night"
}

func daypartColumns(parts []*daypart) twcDaypartColumns {
	return twcDaypartColumns{
		DaypartName:           column(parts, func(p daypart) string { return p.name }),
		DayOrNight:            column(parts, func(p daypart) string { return p.dayOrNight }),
		IconCode:              column(parts, func(p daypart) int { return p.iconCode }),
		WxPhraseLong:          column(parts, func(p daypart) string { return p.phraseLong }),
		WxPhraseShort:         column(parts, func(p daypart) string { return p.phraseShort }),
		Temperature:           column(parts, func(p daypart) int { return p.temperature }),
		PrecipChance:          column(parts, func(p daypart) int { return p.precipChance }),
		WindSpeed:             column(parts, func(p daypart) int { return p.windSpeed }),
		WindDirectionCardinal: column(parts, func(p daypart) string { return p.windCardinal }),
		Narrative:             column(parts, func(p daypart) string { return p.narrative }),
	}
}

// column extracts one field from every daypart, keeping nil parts as nil entries.
func column[T any](parts []*daypart, field func(daypart) T) []*T {
	out := make([]*T, len(parts))
	for i, p := range parts {
		if p != nil {
			value := field(*p)
			out[i] = &value
		}
	}
	return out
}

func dayOrNight(isDay bool) string {
	if isDay {
		return "D"
	}
	return "N"
}

func sentenceCase(phrase string) string {
	if phrase == "" {
		return phrase
	}
	return phrase[:1] + strings.ToLower(phrase[1:])
}
