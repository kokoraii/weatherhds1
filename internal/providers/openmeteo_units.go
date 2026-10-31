package providers

import (
	"fmt"
	"math"
	"strconv"
)

// units is a TWC units code from serverConfig.units. Open-Meteo is always requested in metric and
// converted here: "e" imperial, "m" metric, "h" hybrid (°C with mph and miles), "s" SI (m/s winds).
type units string

func (u units) temp(celsius float64) int {
	if u == "e" {
		return int(math.Round(celsius*9/5 + 32))
	}
	return int(math.Round(celsius))
}

func (u units) wind(kmh float64) int {
	return int(math.Round(u.windValue(kmh)))
}

func (u units) windValue(kmh float64) float64 {
	switch u {
	case "e", "h":
		return kmh / 1.609344
	case "s":
		return kmh / 3.6
	default:
		return kmh
	}
}

// windRounded rounds a wind speed to multiples of the step forecasts use (5 km/h or mph, 1 m/s);
// steps=2 gives the coarser 10 km/h steps used for gusts.
func (u units) windRounded(kmh float64, steps int) int {
	step := 5.0
	if u == "s" {
		step = 1
	}
	step *= float64(steps)
	return int(math.Round(u.windValue(kmh)/step) * step)
}

// distance converts metres to miles or kilometres, one decimal like TWC.
func (u units) distance(metres float64) float64 {
	if u == "e" || u == "h" {
		return roundTo(metres/1609.344, 1)
	}
	return roundTo(metres/1000, 1)
}

// pressure converts hPa to inches of mercury for imperial, otherwise keeps millibars.
func (u units) pressure(hpa float64) float64 {
	if u == "e" {
		return roundTo(hpa*0.0295300, 2)
	}
	return roundTo(hpa, 1)
}

func (u units) precip(mm float64) float64 {
	if u == "e" {
		return roundTo(mm/25.4, 2)
	}
	return roundTo(mm, 1)
}

// spokenTemp writes a temperature the way Environment Canada forecasts do: "minus 6", "zero",
// "plus 3" (small positive Celsius values), or a bare number.
func (u units) spokenTemp(celsius float64) string {
	value := u.temp(celsius)
	switch {
	case value < 0:
		return fmt.Sprintf("minus %d", -value)
	case value == 0:
		return "zero"
	case value <= 5 && u != "e":
		return fmt.Sprintf("plus %d", value)
	default:
		return strconv.Itoa(value)
	}
}

// snowAmount converts a snowfall total to display units, with the step amounts are rounded to.
func (u units) snowAmount(cm float64) (value, step float64, label string) {
	if u == "e" {
		return cm / 2.54, 0.5, "in"
	}
	return cm, 1, "cm"
}

// rainAmount converts a rainfall total to display units, with the step amounts are rounded to.
func (u units) rainAmount(mm float64) (value, step float64, label string) {
	if u == "e" {
		return mm / 25.4, 0.1, "in"
	}
	return mm, 1, "mm"
}

// tempLetter is the unit letter TWC narratives use ("High 12C").
func (u units) tempLetter() string {
	if u == "e" {
		return "F"
	}
	return "C"
}

func (u units) windLabel() string {
	switch u {
	case "e", "h":
		return "mph"
	case "s":
		return "m/s"
	default:
		return "km/h"
	}
}

func roundTo(value float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	return math.Round(value*scale) / scale
}
