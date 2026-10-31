package providers

import "math"

// omPollutants maps TWC pollutant names to Open-Meteo's US AQI sub-index and concentration variables.
var omPollutants = []struct {
	name, indexField, amountField string
}{
	{"PM2.5", "us_aqi_pm2_5", "pm2_5"},
	{"PM10", "us_aqi_pm10", "pm10"},
	{"O3", "us_aqi_ozone", "ozone"},
	{"NO2", "us_aqi_nitrogen_dioxide", "nitrogen_dioxide"},
	{"SO2", "us_aqi_sulphur_dioxide", "sulphur_dioxide"},
	{"CO", "us_aqi_carbon_monoxide", "carbon_monoxide"},
}

func omAirQualityFields() string {
	fields := []string{"us_aqi"}
	for _, p := range omPollutants {
		fields = append(fields, p.indexField, p.amountField)
	}
	return joinFields(fields...)
}

// omAirQuality is the Open-Meteo air quality response; Current is keyed by the requested variables.
type omAirQuality struct {
	Current map[string]float64 `json:"current"`
}

// epaCategories are the EPA AQI bands with their standard colours and general health messages.
var epaCategories = []struct {
	upTo    int
	name    string
	color   string
	message string
}{
	{50, "Good", "00E400", "Air quality is satisfactory, and air pollution poses little or no risk."},
	{100, "Moderate", "FFFF00", "Air quality is acceptable. However, there may be a risk for some people, particularly those who are unusually sensitive to air pollution."},
	{150, "Unhealthy for Sensitive Groups", "FF7E00", "Members of sensitive groups may experience health effects. The general public is less likely to be affected."},
	{200, "Unhealthy", "FF0000", "Some members of the general public may experience health effects; members of sensitive groups may experience more serious health effects."},
	{300, "Very Unhealthy", "8F3F97", "Health alert: The risk of health effects is increased for everyone."},
	{math.MaxInt, "Hazardous", "7E0023", "Health warning of emergency conditions: everyone is more likely to be affected."},
}

// toTWC reshapes the reading into TWC's globalairquality object. The primary pollutant is the one
// with the highest sub-index, which is what sets the overall US AQI.
func (a omAirQuality) toTWC() twcGlobalAirQuality {
	index := int(math.Round(a.Current["us_aqi"]))
	out := twcGlobalAirQuality{AirQualityIndex: index, Pollutants: map[string]twcPollutant{}}
	primaryIndex := -1
	for _, p := range omPollutants {
		pollutant := twcPollutant{
			Amount: roundTo(a.Current[p.amountField], 1),
			Unit:   "µg/m3",
			Index:  int(math.Round(a.Current[p.indexField])),
		}
		out.Pollutants[p.name] = pollutant
		if pollutant.Index > primaryIndex {
			primaryIndex = pollutant.Index
			out.PrimaryPollutant = p.name
		}
	}
	for i, category := range epaCategories {
		if index <= category.upTo {
			out.AirQualityCategory = category.name
			out.AirQualityCategoryIndex = i + 1
			out.AirQualityCategoryIndexColor = category.color
			out.Messages.General.Text = category.message
			break
		}
	}
	return out
}
