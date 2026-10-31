package providers

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Short-term narratives: for the first few dayparts, describe how the hours change instead of
// summarizing them, in Environment Canada's wording, e.g.
//
//	Cloudy. 60 percent chance of flurries this evening. Wind north 20 km/h gusting to 40 becoming
//	light after midnight. Low minus 6. Wind chill minus 14 overnight.

const (
	// shortTermParts is how many dayparts get a detailed narrative: the periods the short-term slide pages through.
	shortTermParts = 4
	// chancePercent and definitePercent bound "X percent chance of rain" versus a plain "Rain.".
	chancePercent   = 30
	definitePercent = 70
	// windUpToKmh is the speed below which wind reads "Wind up to 15 km/h."
	windUpToKmh = 15
	// trendCelsius is how far temperature must move after the high (or low) to be called out.
	trendCelsius = 3
	// snowAmountCm and rainAmountMm are the smallest totals worth stating.
	snowAmountCm = 1
	rainAmountMm = 4
)

// Time phrases for the four 3-hour blocks of a daypart, and for its two halves. "{}" becomes "this"
// for Today and Tonight and "in the" for later periods ("early this morning", "early in the morning").
var (
	dayBlockLabels   = [4]string{"early {} morning", "late {} morning", "early {} afternoon", "late {} afternoon"}
	nightBlockLabels = [4]string{"early {} evening", "late {} evening", "after midnight", "toward morning"}
	dayHalfLabels    = [2]string{"{} morning", "{} afternoon"}
	nightHalfLabels  = [2]string{"{} evening", "after midnight"}
)

var skyWords = map[bool][5]string{ // keyed by daytime
	true:  {"Sunny", "Mainly sunny", "A mix of sun and cloud", "Mainly cloudy", "Cloudy"},
	false: {"Clear", "A few clouds", "Partly cloudy", "Mainly cloudy", "Cloudy"},
}

var compassNames = [...]string{"north", "northeast", "east", "southeast", "south", "southwest", "west", "northwest"}

// hourBlock is one 3-hour block of a daypart: slot 0-3 within the period, and its hourly index range.
type hourBlock struct {
	slot, from, to int
}

// narrativePeriod is the not-yet-elapsed part of one daypart, split into its remaining blocks.
type narrativePeriod struct {
	b       forecastBuilder
	daytime bool
	current bool // Today or Tonight
	blocks  []hourBlock
	uvMax   float64
}

// newNarrativePeriod splits the daypart starting at start (07:00 or 19:00) into 3-hour blocks,
// skipping blocks that have already ended and trimming the one in progress.
func (b forecastBuilder) newNarrativePeriod(start time.Time, daytime, current bool, uvMax float64) narrativePeriod {
	p := narrativePeriod{b: b, daytime: daytime, current: current, uvMax: uvMax}
	year, month, day := start.Date()
	for slot := range 4 {
		blockStart := time.Date(year, month, day, start.Hour()+3*slot, 0, 0, 0, b.f.location)
		blockEnd := time.Date(year, month, day, start.Hour()+3*slot+3, 0, 0, 0, b.f.location)
		if !blockEnd.After(b.now) {
			continue
		}
		if blockStart.Before(b.now) {
			blockStart = b.now
		}
		p.blocks = append(p.blocks, hourBlock{slot: slot, from: b.hourIndex(blockStart), to: b.hourIndex(blockEnd)})
	}
	return p
}

// text assembles the narrative: sky, precipitation and amounts, sky change, fog, wind, temperature,
// wind chill or humidex, UV. Clauses with nothing to say are left out.
func (p narrativePeriod) text() string {
	sky, skyChange := p.sky()
	precip, replacesSky := p.precipitation()
	var sentences []string
	if replacesSky {
		skyChange = ""
	} else {
		sentences = append(sentences, sky+".")
	}
	sentences = append(sentences, precip...)
	for _, sentence := range []string{skyChange, p.fog(), p.wind(), p.temperature(), p.feelsLike(), p.uv()} {
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
	}
	return strings.Join(sentences, " ")
}

// sky names the cloud cover, plus "Clearing ..." or "Increasing cloudiness ..." when it swings
// by two or more categories across the period.
func (p narrativePeriod) sky() (opening, change string) {
	h := p.b.f.Hourly
	categories := make([]int, len(p.blocks))
	for i, block := range p.blocks {
		categories[i] = skyCategory(mean(h.CloudCover[block.from:block.to]))
	}
	first, last := categories[0], categories[len(categories)-1]
	words := skyWords[p.daytime]
	for i, category := range categories {
		switch {
		case last-first >= 2 && category-first >= 2:
			return words[first], "Increasing cloudiness " + p.pointLabel(i) + "."
		case first-last >= 2 && first-category >= 2:
			return words[first], "Clearing " + p.pointLabel(i) + "."
		}
	}
	return words[skyCategory(mean(h.CloudCover[p.from():p.to()]))], ""
}

func skyCategory(cloudCover float64) int {
	switch {
	case cloudCover < 15:
		return 0
	case cloudCover < 40:
		return 1
	case cloudCover < 65:
		return 2
	case cloudCover < 90:
		return 3
	default:
		return 4
	}
}

type precipKind int

// Ordered by severity, which breaks ties between kinds lasting equally long.
const (
	noPrecip precipKind = iota
	drizzle
	rain
	showers
	flurries
	snow
	freezingDrizzle
	freezingRain
	thunderstorms
)

var precipWords = map[precipKind]string{
	drizzle: "drizzle", rain: "rain", showers: "showers", flurries: "flurries", snow: "snow",
	freezingDrizzle: "freezing drizzle", freezingRain: "freezing rain", thunderstorms: "thunderstorms",
}

func precipKindFor(code int) precipKind {
	switch code {
	case 51, 53, 55:
		return drizzle
	case 61, 63, 65:
		return rain
	case 80, 81, 82:
		return showers
	case 71, 77, 85:
		return flurries
	case 73, 75, 86:
		return snow
	case 56, 57:
		return freezingDrizzle
	case 66, 67:
		return freezingRain
	case 95, 96, 99:
		return thunderstorms
	}
	return noPrecip
}

func (k precipKind) frozen() bool {
	return k == flurries || k == snow
}

// steady kinds fall continuously, so intermittent ones read "Periods of rain"; showers are already intermittent.
func (k precipKind) steady() bool {
	return k == drizzle || k == rain || k == snow
}

func isHeavyPrecip(code int) bool {
	return code == 65 || code == 75 || code == 82 || code == 86
}

// precipitation describes when precipitation is expected and how likely it is, followed by amounts.
// replacesSky is true for precipitation lasting the whole period, which makes a sky clause redundant.
func (p narrativePeriod) precipitation() (sentences []string, replacesSky bool) {
	h := p.b.f.Hourly
	active := make([]bool, len(p.blocks))
	hours := map[precipKind]int{}
	var chance float64
	codeHours, heavyHours, peakHour := 0, 0, p.from()
	for i, block := range p.blocks {
		for hour := block.from; hour < block.to; hour++ {
			if kind := precipKindFor(h.WeatherCode[hour]); kind != noPrecip {
				hours[kind]++
				codeHours++
				active[i] = true
			}
			if isHeavyPrecip(h.WeatherCode[hour]) {
				heavyHours++
			}
			if h.PrecipitationProbability[hour] >= chancePercent {
				active[i] = true
			}
			if h.PrecipitationProbability[hour] > chance {
				chance, peakHour = h.PrecipitationProbability[hour], hour
			}
		}
	}
	first, last := slices.Index(active, true), len(active)-1-slices.Index(reversed(active), true)
	if first < 0 {
		return nil, false
	}
	if codeHours >= 2 {
		chance = max(chance, chancePercent) // the model forecasts precipitation even if the ensemble doubts it
	}
	percent := int(math.Round(chance/10) * 10)
	if percent < chancePercent {
		return nil, false
	}

	snowCm, rainMm := p.totals()
	primary, secondary := dominantPrecip(hours)
	switch {
	case primary == noPrecip && h.Temperature[peakHour] <= 0:
		primary = flurries
	case primary == noPrecip:
		primary = showers
	case primary == drizzle && rainMm >= rainAmountMm:
		primary = rain // Open-Meteo codes steady light rain as drizzle; a measurable total is rain
	}
	coversAll := first == 0 && last == len(p.blocks)-1
	var sentence string
	if percent >= definitePercent {
		subject := precipWords[primary]
		if secondary != noPrecip {
			subject += " mixed with " + precipWords[secondary]
		}
		spanHours := p.blocks[last].to - p.blocks[first].from
		if primary.steady() && codeHours*10 < spanHours*6 {
			subject = "periods of " + subject
		}
		if heavyHours >= 2 {
			subject += " at times heavy"
		}
		subject = capitalize(subject)
		switch {
		case coversAll:
			sentence = subject + "."
		case last == len(p.blocks)-1:
			sentence = subject + " beginning " + p.pointLabel(first) + "."
		case first == 0:
			sentence = subject + " ending " + p.blockLabel(last+1) + "."
		default:
			sentence = subject + " " + p.spanLabel(first, last) + "."
		}
	} else {
		subject := precipWords[primary]
		if secondary != noPrecip {
			subject += " or " + precipWords[secondary]
		}
		sentence = fmt.Sprintf("%d percent chance of %s", percent, subject)
		if !coversAll {
			sentence += " " + p.spanLabel(first, last)
		}
		sentence += "."
	}
	sentences = append([]string{sentence}, p.amounts(snowCm, rainMm, primary, secondary)...)
	return sentences, percent >= definitePercent && coversAll
}

// dominantPrecip returns the longest-lasting kind and, when it lasts two hours or more, a second kind
// of the other phase (snow with rain), so mixed periods read "rain or snow".
func dominantPrecip(hours map[precipKind]int) (primary, secondary precipKind) {
	kinds := make([]precipKind, 0, len(hours))
	for kind := range hours {
		kinds = append(kinds, kind)
	}
	slices.SortFunc(kinds, func(a, b precipKind) int {
		if hours[a] != hours[b] {
			return hours[b] - hours[a]
		}
		return int(b - a)
	})
	if len(kinds) == 0 {
		return noPrecip, noPrecip
	}
	for _, kind := range kinds[1:] {
		if hours[kind] >= 2 && kind.frozen() != kinds[0].frozen() {
			return kinds[0], kind
		}
	}
	return kinds[0], noPrecip
}

// totals sums the period's snowfall (cm) and rain plus showers (mm).
func (p narrativePeriod) totals() (snowCm, rainMm float64) {
	h := p.b.f.Hourly
	for hour := p.from(); hour < p.to(); hour++ {
		snowCm += h.Snowfall[hour]
		rainMm += h.Rain[hour] + h.Showers[hour]
	}
	return snowCm, rainMm
}

// amounts reports snowfall and rainfall totals that reach snowAmountCm or rainAmountMm, for the
// kinds of precipitation the forecast names.
func (p narrativePeriod) amounts(snowCm, rainMm float64, kinds ...precipKind) []string {
	frozen := slices.ContainsFunc(kinds, precipKind.frozen)
	liquid := slices.ContainsFunc(kinds, func(k precipKind) bool { return k != noPrecip && !k.frozen() })
	var out []string
	if frozen && snowCm >= snowAmountCm {
		out = append(out, "Snowfall amount "+amountRange(p.b.units.snowAmount(snowCm))+".")
	}
	if liquid && rainMm >= rainAmountMm {
		out = append(out, "Rainfall amount "+amountRange(p.b.units.rainAmount(rainMm))+".")
	}
	return out
}

// amountRange gives a forecast-style amount: "2 to 4 cm", or "5 cm" when the range collapses.
func amountRange(value, step float64, label string) string {
	low := max(step, math.Round(value*0.75/step)*step)
	high := math.Round(value*1.25/step) * step
	if high <= low {
		return formatAmount(low) + " " + label
	}
	return formatAmount(low) + " to " + formatAmount(high) + " " + label
}

func formatAmount(value float64) string {
	return strconv.FormatFloat(roundTo(value, 1), 'f', -1, 64)
}

// fog reports two or more hours of visibility under 1 km (or a fog weather code).
func (p narrativePeriod) fog() string {
	h := p.b.f.Hourly
	active := make([]bool, len(p.blocks))
	foggyHours, freezing := 0, false
	for i, block := range p.blocks {
		for hour := block.from; hour < block.to; hour++ {
			code := h.WeatherCode[hour]
			if h.Visibility[hour] < 1000 || code == 45 || code == 48 {
				foggyHours++
				active[i] = true
				freezing = freezing || code == 48 || h.Temperature[hour] <= 0
			}
		}
	}
	if foggyHours < 2 {
		return ""
	}
	subject := "Fog patches"
	if freezing {
		subject = "Freezing fog patches"
	}
	first, last := slices.Index(active, true), len(active)-1-slices.Index(reversed(active), true)
	switch {
	case first == 0 && last == len(p.blocks)-1:
		return subject + "."
	case first == 0:
		return subject + " dissipating " + p.blockLabel(last+1) + "."
	case last == len(p.blocks)-1:
		return subject + " developing " + p.pointLabel(first) + "."
	default:
		return subject + " " + p.spanLabel(first, last) + "."
	}
}

type windStats struct {
	meanKmh, maxKmh, gustKmh, direction float64
}

func (s windStats) light() bool {
	return s.meanKmh < windUpToKmh
}

// changesTo reports a wind change worth a "becoming": picking up, dying down, swinging to another
// compass point, or a 10 km/h change in speed.
func (s windStats) changesTo(next windStats) bool {
	if s.light() || next.light() {
		return s.light() != next.light()
	}
	return compassName(s.direction) != compassName(next.direction) || math.Abs(s.meanKmh-next.meanKmh) >= 10
}

func (p narrativePeriod) windStats(firstBlock, lastBlock int) windStats {
	h := p.b.f.Hourly
	from, to := p.blocks[firstBlock].from, p.blocks[lastBlock].to
	speed, direction := meanWind(h.WindSpeed[from:to], h.WindDirection[from:to])
	return windStats{meanKmh: speed, maxKmh: slices.Max(h.WindSpeed[from:to]), gustKmh: slices.Max(h.WindGusts[from:to]), direction: direction}
}

// wind compares the two halves of the period: "Wind north 20 km/h becoming northwest 40 gusting to 60 this afternoon."
func (p narrativePeriod) wind() string {
	u := p.b.units
	if count := len(p.blocks); count > 1 {
		split := (count + 1) / 2
		first, second := p.windStats(0, split-1), p.windStats(split, count-1)
		if first.changesTo(second) {
			when := p.pointLabel(split)
			switch {
			case first.light():
				return fmt.Sprintf("Wind becoming %s %s.", p.describeWind(second, true), when)
			case second.light():
				return fmt.Sprintf("Wind %s becoming light %s.", p.describeWind(first, true), when)
			default:
				return fmt.Sprintf("Wind %s becoming %s %s.", p.describeWind(first, true), p.describeWind(second, false), when)
			}
		}
	}
	whole := p.windStats(0, len(p.blocks)-1)
	if whole.light() {
		return fmt.Sprintf("Wind up to %d %s.", u.windRounded(windUpToKmh, 1), u.windLabel())
	}
	return "Wind " + p.describeWind(whole, true) + "."
}

// describeWind writes "northwest 20 to 30 km/h gusting to 50"; a range appears when the peak runs
// 10 km/h past the mean, and gusts when they reach 40 km/h and beat the peak by 10.
func (p narrativePeriod) describeWind(s windStats, withUnit bool) string {
	u := p.b.units
	speed, peak := u.windRounded(s.meanKmh, 1), u.windRounded(s.maxKmh, 1)
	text := fmt.Sprintf("%s %d", compassName(s.direction), speed)
	if peak-speed >= u.windRounded(10, 1) {
		text += fmt.Sprintf(" to %d", peak)
	}
	if withUnit {
		text += " " + u.windLabel()
	}
	if s.gustKmh >= 40 && s.gustKmh >= s.maxKmh+10 {
		text += fmt.Sprintf(" gusting to %d", u.windRounded(s.gustKmh, 2))
	}
	return text
}

func compassName(degrees float64) string {
	return compassNames[int(math.Mod(degrees+22.5, 360)/45)%len(compassNames)]
}

// temperature gives the high or low, and calls out a high that comes early before temperatures fall,
// or a low in the evening before temperatures rise overnight.
func (p narrativePeriod) temperature() string {
	u := p.b.units
	temps := p.b.f.Hourly.Temperature[p.from():p.to()]
	end := temps[len(temps)-1]
	if p.daytime {
		peak := slices.Index(temps, slices.Max(temps))
		if block := p.blockOf(p.from() + peak); block == 0 && len(p.blocks) >= 3 && temps[peak]-end >= trendCelsius {
			last := p.blocks[len(p.blocks)-1].slot
			return fmt.Sprintf("High %s %s. Temperature falling to %s %s.", u.spokenTemp(temps[peak]), p.pointLabel(0), u.spokenTemp(end), p.halfLabel(last))
		}
		return "High " + u.spokenTemp(temps[peak]) + "."
	}
	low := slices.Index(temps, slices.Min(temps))
	if block := p.blockOf(p.from() + low); p.blocks[block].slot < 2 && end-temps[low] >= trendCelsius {
		return fmt.Sprintf("Low %s %s. Temperature rising to %s by morning.", u.spokenTemp(temps[low]), p.halfLabel(p.blocks[block].slot), u.spokenTemp(end))
	}
	return "Low " + u.spokenTemp(temps[low]) + "."
}

// feelsLike reports a wind chill of -10 or colder, or a humidex (heat index) of 30 or more, when the
// apparent temperature differs enough from the air temperature to matter.
func (p narrativePeriod) feelsLike() string {
	u := p.b.units
	h := p.b.f.Hourly
	from, to := p.from(), p.to()
	apparent, temps := h.ApparentTemperature[from:to], h.Temperature[from:to]
	coldest := slices.Index(apparent, slices.Min(apparent))
	if apparent[coldest] <= -10 && apparent[coldest] <= temps[coldest]-4 {
		slot := p.blocks[p.blockOf(from+coldest)].slot
		when := p.halfLabel(slot)
		if !p.daytime && slot >= 2 {
			when = "overnight"
		}
		return fmt.Sprintf("Wind chill %s %s.", u.spokenTemp(apparent[coldest]), when)
	}
	hottest := slices.Index(apparent, slices.Max(apparent))
	if apparent[hottest] >= 30 && apparent[hottest] >= temps[hottest]+3 {
		label := "Humidex"
		if u == "e" {
			label = "Heat index"
		}
		return fmt.Sprintf("%s %d.", label, u.temp(apparent[hottest]))
	}
	return ""
}

func (p narrativePeriod) uv() string {
	index := int(math.Round(p.uvMax))
	if !p.daytime || index < 1 {
		return ""
	}
	return fmt.Sprintf("UV index %d or %s.", index, strings.ToLower(uvDescription(p.uvMax)))
}

func (p narrativePeriod) from() int {
	return p.blocks[0].from
}

func (p narrativePeriod) to() int {
	return p.blocks[len(p.blocks)-1].to
}

// blockOf returns the position in p.blocks of the block holding an hourly index.
func (p narrativePeriod) blockOf(hour int) int {
	for i, block := range p.blocks {
		if hour < block.to {
			return i
		}
	}
	return len(p.blocks) - 1
}

func (p narrativePeriod) fill(template string) string {
	article := "in the"
	if p.current {
		article = "this"
	}
	return strings.Replace(template, "{}", article, 1)
}

func (p narrativePeriod) blockLabel(block int) string {
	labels := nightBlockLabels
	if p.daytime {
		labels = dayBlockLabels
	}
	return p.fill(labels[p.blocks[block].slot])
}

// halfLabel takes a slot (0-3), not a block position, since temperature and wind chill phrases
// name the half an hour falls in.
func (p narrativePeriod) halfLabel(slot int) string {
	labels := nightHalfLabels
	if p.daytime {
		labels = dayHalfLabels
	}
	return p.fill(labels[slot/2])
}

// pointLabel names when a change happens: the half's name when it starts a half ("this afternoon"),
// otherwise the block's ("late this afternoon").
func (p narrativePeriod) pointLabel(block int) string {
	if slot := p.blocks[block].slot; slot%2 == 0 {
		return p.halfLabel(slot)
	}
	return p.blockLabel(block)
}

// spanLabel names a run of blocks: one block, a whole half, or "late this morning and this afternoon".
func (p narrativePeriod) spanLabel(first, last int) string {
	firstSlot, lastSlot := p.blocks[first].slot, p.blocks[last].slot
	switch {
	case first == last:
		return p.blockLabel(first)
	case firstSlot/2 == lastSlot/2:
		return p.halfLabel(firstSlot)
	}
	start, end := p.blockLabel(first), p.blockLabel(last)
	if firstSlot%2 == 0 {
		start = p.halfLabel(firstSlot)
	}
	if lastSlot%2 == 1 {
		end = p.halfLabel(lastSlot)
	}
	return start + " and " + end
}

func mean(values []float64) float64 {
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func reversed[T any](values []T) []T {
	out := slices.Clone(values)
	slices.Reverse(out)
	return out
}

func capitalize(text string) string {
	return strings.ToUpper(text[:1]) + text[1:]
}
