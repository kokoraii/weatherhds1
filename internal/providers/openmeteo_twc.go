package providers

// TWC-shaped products built from Open-Meteo data. Only the fields the frontend reads are included;
// json names match the weather.com v3/v2 payloads. Pointer fields serialize as null where TWC sends
// null, which the frontend checks for explicitly (e.g. `windGust === null`).

// twcTimeLayout is the local timestamp format TWC uses, e.g. 2026-10-30T07:43:00-0600.
const twcTimeLayout = "2006-01-02T15:04:05-0700"

// twcObservation mirrors "v3-wx-observations-current".
type twcObservation struct {
	ValidTimeLocal          string  `json:"validTimeLocal"`
	DayOfWeek               string  `json:"dayOfWeek"`
	DayOrNight              string  `json:"dayOrNight"`
	IconCode                int     `json:"iconCode"`
	WxPhraseLong            string  `json:"wxPhraseLong"`
	WxPhraseMedium          string  `json:"wxPhraseMedium"`
	WxPhraseShort           string  `json:"wxPhraseShort"`
	Temperature             int     `json:"temperature"`
	TemperatureFeelsLike    int     `json:"temperatureFeelsLike"`
	TemperatureDewPoint     int     `json:"temperatureDewPoint"`
	TemperatureChange24Hour int     `json:"temperatureChange24Hour"`
	TemperatureMax24Hour    int     `json:"temperatureMax24Hour"`
	TemperatureMin24Hour    int     `json:"temperatureMin24Hour"`
	RelativeHumidity        int     `json:"relativeHumidity"`
	WindSpeed               int     `json:"windSpeed"`
	WindDirection           int     `json:"windDirection"`
	WindDirectionCardinal   string  `json:"windDirectionCardinal"`
	WindGust                *int    `json:"windGust"`
	PressureAltimeter       float64 `json:"pressureAltimeter"`
	PressureTendencyCode    int     `json:"pressureTendencyCode"`
	PressureTendencyTrend   string  `json:"pressureTendencyTrend"`
	Visibility              float64 `json:"visibility"`
	UVIndex                 int     `json:"uvIndex"`
	UVDescription           string  `json:"uvDescription"`
	CloudCeiling            *int    `json:"cloudCeiling"` // always null: Open-Meteo has no ceiling, shown as "Unlimited"
	Precip24Hour            float64 `json:"precip24Hour"`
	SunriseTimeLocal        string  `json:"sunriseTimeLocal"`
	SunsetTimeLocal         string  `json:"sunsetTimeLocal"`
}

// twcDailyForecast mirrors "v3-wx-forecast-daily-7day" / "-3day": one entry per calendar day starting
// today, with Daypart holding two entries (day, night) per calendar day.
type twcDailyForecast struct {
	DayOfWeek                 []string            `json:"dayOfWeek"`
	ValidTimeLocal            []string            `json:"validTimeLocal"`
	CalendarDayTemperatureMax []int               `json:"calendarDayTemperatureMax"`
	CalendarDayTemperatureMin []int               `json:"calendarDayTemperatureMin"`
	Qpf                       []float64           `json:"qpf"`
	SunriseTimeLocal          []string            `json:"sunriseTimeLocal"`
	SunsetTimeLocal           []string            `json:"sunsetTimeLocal"`
	MoonriseTimeLocal         []*string           `json:"moonriseTimeLocal"`
	MoonsetTimeLocal          []*string           `json:"moonsetTimeLocal"`
	MoonPhase                 []string            `json:"moonPhase"`
	MoonPhaseCode             []string            `json:"moonPhaseCode"`
	Daypart                   []twcDaypartColumns `json:"daypart"`
}

// twcDaypartColumns is TWC's column layout for dayparts; a nil entry is a daypart that has already
// passed (TWC drops "Today" in the afternoon).
type twcDaypartColumns struct {
	DaypartName           []*string `json:"daypartName"`
	DayOrNight            []*string `json:"dayOrNight"`
	IconCode              []*int    `json:"iconCode"`
	WxPhraseLong          []*string `json:"wxPhraseLong"`
	WxPhraseShort         []*string `json:"wxPhraseShort"`
	Temperature           []*int    `json:"temperature"`
	PrecipChance          []*int    `json:"precipChance"`
	WindSpeed             []*int    `json:"windSpeed"`
	WindDirectionCardinal []*string `json:"windDirectionCardinal"`
	Narrative             []*string `json:"narrative"`
}

// twcHourlyForecast mirrors "v3-wx-forecast-hourly-2day", starting with the next hour.
type twcHourlyForecast struct {
	ValidTimeLocal        []string `json:"validTimeLocal"`
	DayOrNight            []string `json:"dayOrNight"`
	IconCode              []int    `json:"iconCode"`
	WxPhraseLong          []string `json:"wxPhraseLong"`
	WxPhraseShort         []string `json:"wxPhraseShort"`
	Temperature           []int    `json:"temperature"`
	PrecipChance          []int    `json:"precipChance"`
	WindSpeed             []int    `json:"windSpeed"`
	WindDirectionCardinal []string `json:"windDirectionCardinal"`
}

// twcIntradayPeriod mirrors one entry of "v2fcstintraday3".forecasts.
type twcIntradayPeriod struct {
	DaypartName    string `json:"daypart_name"`
	FcstValidLocal string `json:"fcst_valid_local"`
	Phrase22Char   string `json:"phrase_22char"`
	Temp           int    `json:"temp"`
	IconCode       int    `json:"icon_code"`
}

// twcGlobalAirQuality mirrors "v3-wx-globalAirQuality".globalairquality on the EPA scale.
type twcGlobalAirQuality struct {
	AirQualityIndex              int                     `json:"airQualityIndex"`
	AirQualityCategory           string                  `json:"airQualityCategory"`
	AirQualityCategoryIndex      int                     `json:"airQualityCategoryIndex"`
	AirQualityCategoryIndexColor string                  `json:"airQualityCategoryIndexColor"`
	PrimaryPollutant             string                  `json:"primaryPollutant"`
	Pollutants                   map[string]twcPollutant `json:"pollutants"`
	Messages                     twcAirQualityMessages   `json:"messages"`
}

type twcPollutant struct {
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
	Index  int     `json:"index"`
}

type twcAirQualityMessages struct {
	General struct {
		Text string `json:"text"`
	} `json:"General"`
}
