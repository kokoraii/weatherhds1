package providers

import "math"

type phrases struct {
	long, medium, short string
}

// wmoCondition is how one WMO weather code is presented: TWC icon codes (keys of weatherIcons in
// config.js) and phrases, for day and for night.
type wmoCondition struct {
	dayIcon, nightIcon int
	day, night         phrases
}

func (c wmoCondition) at(isDay bool) (int, phrases) {
	if isDay {
		return c.dayIcon, c.day
	}
	return c.nightIcon, c.night
}

// sameDayNight is a condition phrased the same day and night.
func sameDayNight(dayIcon, nightIcon int, long, medium, short string) wmoCondition {
	p := phrases{long, medium, short}
	return wmoCondition{dayIcon, nightIcon, p, p}
}

var wmoConditions = map[int]wmoCondition{
	0:  {32, 31, phrases{"Sunny", "Sunny", "Sunny"}, phrases{"Clear", "Clear", "Clear"}},
	1:  {34, 33, phrases{"Mostly Sunny", "Mostly Sunny", "M Sunny"}, phrases{"Mostly Clear", "Mostly Clear", "M Clear"}},
	2:  sameDayNight(30, 29, "Partly Cloudy", "Partly Cloudy", "P Cloudy"),
	3:  sameDayNight(26, 26, "Cloudy", "Cloudy", "Cloudy"),
	45: sameDayNight(20, 20, "Fog", "Fog", "Fog"),
	48: sameDayNight(20, 20, "Freezing Fog", "Freezing Fog", "Frz Fog"),
	51: sameDayNight(9, 9, "Light Drizzle", "Light Drizzle", "Lt Drizzle"),
	53: sameDayNight(9, 9, "Drizzle", "Drizzle", "Drizzle"),
	55: sameDayNight(9, 9, "Heavy Drizzle", "Heavy Drizzle", "Hvy Drizzle"),
	56: sameDayNight(8, 8, "Light Freezing Drizzle", "Freezing Drizzle", "Frz Drizzle"),
	57: sameDayNight(8, 8, "Freezing Drizzle", "Freezing Drizzle", "Frz Drizzle"),
	61: sameDayNight(11, 11, "Light Rain", "Light Rain", "Lt Rain"),
	63: sameDayNight(12, 12, "Rain", "Rain", "Rain"),
	65: sameDayNight(40, 40, "Heavy Rain", "Heavy Rain", "Hvy Rain"),
	66: sameDayNight(10, 10, "Light Freezing Rain", "Freezing Rain", "Frz Rain"),
	67: sameDayNight(10, 10, "Freezing Rain", "Freezing Rain", "Frz Rain"),
	71: sameDayNight(14, 14, "Light Snow", "Light Snow", "Lt Snow"),
	73: sameDayNight(16, 16, "Snow", "Snow", "Snow"),
	75: sameDayNight(42, 42, "Heavy Snow", "Heavy Snow", "Hvy Snow"),
	77: sameDayNight(13, 13, "Snow Grains", "Snow Grains", "Snow Grains"),
	80: sameDayNight(39, 45, "Scattered Showers", "Sct Showers", "Sct Showers"),
	81: sameDayNight(11, 11, "Showers", "Showers", "Showers"),
	82: sameDayNight(40, 40, "Heavy Showers", "Heavy Showers", "Hvy Showers"),
	85: sameDayNight(41, 46, "Scattered Snow Showers", "Sct Snow Showers", "Sct Snow"),
	86: sameDayNight(42, 42, "Heavy Snow Showers", "Hvy Snow Showers", "Hvy Snow"),
	95: sameDayNight(4, 47, "Thunderstorms", "Thunderstorms", "T-Storms"),
	96: sameDayNight(4, 47, "Thunderstorms with Hail", "T-Storms and Hail", "T-Storms"),
	99: sameDayNight(3, 3, "Thunderstorms with Heavy Hail", "Severe T-Storms", "Svr T-Storms"),
}

var unknownCondition = sameDayNight(44, 44, "Not Available", "N/A", "N/A")

func conditionFor(code int) wmoCondition {
	if condition, ok := wmoConditions[code]; ok {
		return condition
	}
	return unknownCondition
}

// dominantCode picks the WMO code that represents a period: the most severe precipitation or fog code
// lasting two or more hours (a single thunderstorm hour is enough), otherwise the average sky cover.
// WMO codes grow with severity, so "most severe" is the highest code.
func dominantCode(codes []int) int {
	hours := map[int]int{}
	skySum, skyHours, fallback := 0, 0, 0
	for _, code := range codes {
		if code <= 3 {
			skySum += code
			skyHours++
			continue
		}
		hours[code]++
		fallback = max(fallback, code)
	}
	best := -1
	for code, count := range hours {
		if (count >= 2 || code >= 95) && code > best {
			best = code
		}
	}
	switch {
	case best >= 0:
		return best
	case skyHours > 0:
		return int(math.Round(float64(skySum) / float64(skyHours)))
	default:
		return fallback
	}
}

// meanWind averages speeds and vector-averages directions (so 350° and 10° give 0°, not 180°).
func meanWind(speeds, directions []float64) (speed float64, direction float64) {
	var x, y float64
	for i, s := range speeds {
		speed += s
		radians := directions[i] * math.Pi / 180
		x += s * math.Sin(radians)
		y += s * math.Cos(radians)
	}
	direction = math.Mod(math.Atan2(x, y)*180/math.Pi+360, 360)
	return speed / float64(len(speeds)), direction
}

var cardinals = [...]string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}

func cardinal(degrees float64) string {
	return cardinals[int(math.Mod(degrees+11.25, 360)/22.5)%len(cardinals)]
}

// pressureTendency classifies a 3-hour sea-level pressure change (hPa) into TWC's tendency code and text.
func pressureTendency(change float64) (int, string) {
	switch {
	case change >= 3:
		return 3, "Rising Rapidly"
	case change <= -3:
		return 4, "Falling Rapidly"
	case change >= 1:
		return 1, "Rising"
	case change <= -1:
		return 2, "Falling"
	default:
		return 0, "Steady"
	}
}

func uvDescription(index float64) string {
	switch rounded := math.Round(index); {
	case rounded <= 2:
		return "Low"
	case rounded <= 5:
		return "Moderate"
	case rounded <= 7:
		return "High"
	case rounded <= 10:
		return "Very High"
	default:
		return "Extreme"
	}
}
