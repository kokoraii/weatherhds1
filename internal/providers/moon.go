package providers

import (
	"math"
	"time"
)

// Low-precision lunar ephemeris (ported from suncalc, after "Astronomy on the Personal Computer"),
// good to a few minutes for rise/set times. Open-Meteo has no moon data, so the almanac is computed here.

const (
	degToRad       = math.Pi / 180
	earthObliquity = 23.4397 * degToRad
	julian2000     = 2451545.0
	synodicMonth   = 29.530588853
	// knownNewMoon is the Julian date of the 2000-01-06 18:14 UTC new moon.
	knownNewMoon = 2451550.1
)

func julianDate(t time.Time) float64 {
	return float64(t.UnixMilli())/86400000 + 2440587.5
}

// moonAltitude returns the moon's apparent altitude in radians at t for the given coordinates.
func moonAltitude(t time.Time, lat, lon float64) float64 {
	d := julianDate(t) - julian2000
	meanLongitude := degToRad * (218.316 + 13.176396*d)
	meanAnomaly := degToRad * (134.963 + 13.064993*d)
	argumentOfLatitude := degToRad * (93.272 + 13.229350*d)
	longitude := meanLongitude + degToRad*6.289*math.Sin(meanAnomaly)
	latitude := degToRad * 5.128 * math.Sin(argumentOfLatitude)

	rightAscension := math.Atan2(math.Sin(longitude)*math.Cos(earthObliquity)-math.Tan(latitude)*math.Sin(earthObliquity), math.Cos(longitude))
	declination := math.Asin(math.Sin(latitude)*math.Cos(earthObliquity) + math.Cos(latitude)*math.Sin(earthObliquity)*math.Sin(longitude))
	hourAngle := degToRad*(280.16+360.9856235*d) + degToRad*lon - rightAscension
	phi := degToRad * lat
	altitude := math.Asin(math.Sin(phi)*math.Sin(declination) + math.Cos(phi)*math.Cos(declination)*math.Cos(hourAngle))
	return altitude + refraction(altitude)
}

func refraction(altitude float64) float64 {
	altitude = max(altitude, 0)
	return 0.0002967 / math.Tan(altitude+0.00312536/(altitude+0.08901179))
}

// moonTimes finds moonrise and moonset during the 24 hours from dayStart (local midnight), fitting a
// parabola through altitudes sampled every hour. A zero time means no rise or set that day.
func moonTimes(dayStart time.Time, lat, lon float64) (rise, set time.Time) {
	const horizon = 0.133 * degToRad // moon's apparent radius plus parallax
	altitudeAt := func(hours float64) float64 {
		return moonAltitude(dayStart.Add(time.Duration(hours*float64(time.Hour))), lat, lon) - horizon
	}
	var riseHour, setHour float64
	h0 := altitudeAt(0)
	for i := 1.0; i <= 24; i += 2 {
		h1, h2 := altitudeAt(i), altitudeAt(i+1)
		a := (h0+h2)/2 - h1
		b := (h2 - h0) / 2
		xe := -b / (2 * a)
		ye := (a*xe+b)*xe + h1
		discriminant := b*b - 4*a*h1
		roots, x1, x2 := 0, 0.0, 0.0
		if discriminant >= 0 {
			dx := math.Sqrt(discriminant) / (math.Abs(a) * 2)
			x1, x2 = xe-dx, xe+dx
			if math.Abs(x1) <= 1 {
				roots++
			}
			if math.Abs(x2) <= 1 {
				roots++
			}
			if x1 < -1 {
				x1 = x2
			}
		}
		switch roots {
		case 1:
			if h0 < 0 {
				riseHour = i + x1
			} else {
				setHour = i + x1
			}
		case 2:
			if ye < 0 {
				riseHour, setHour = i+x2, i+x1
			} else {
				riseHour, setHour = i+x1, i+x2
			}
		}
		if riseHour != 0 && setHour != 0 {
			break
		}
		h0 = h2
	}
	toTime := func(hours float64) time.Time {
		if hours == 0 {
			return time.Time{}
		}
		return dayStart.Add(time.Duration(hours * float64(time.Hour)))
	}
	return toTime(riseHour), toTime(setHour)
}

type moonPhase struct {
	code, name string
}

// moonPhaseAt names the phase at t using TWC's codes. The four principal phases get the day
// nearest them; the mean synodic month keeps this within about half a day.
func moonPhaseAt(t time.Time) moonPhase {
	age := math.Mod(julianDate(t)-knownNewMoon, synodicMonth)
	if age < 0 {
		age += synodicMonth
	}
	quarter := synodicMonth / 4
	switch {
	case age < 0.5 || age >= synodicMonth-0.5:
		return moonPhase{"N", "New Moon"}
	case math.Abs(age-quarter) < 0.5:
		return moonPhase{"FQ", "First Quarter"}
	case math.Abs(age-2*quarter) < 0.5:
		return moonPhase{"F", "Full Moon"}
	case math.Abs(age-3*quarter) < 0.5:
		return moonPhase{"LQ", "Last Quarter"}
	case age < quarter:
		return moonPhase{"WXC", "Waxing Crescent"}
	case age < 2*quarter:
		return moonPhase{"WXG", "Waxing Gibbous"}
	case age < 3*quarter:
		return moonPhase{"WNG", "Waning Gibbous"}
	default:
		return moonPhase{"WNC", "Waning Crescent"}
	}
}
