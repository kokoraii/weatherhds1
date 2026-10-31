package providers

import "time"

// Requested variable lists; each must stay in sync with the json tags of the matching struct below.
var (
	omCurrentFields = joinFields("temperature_2m", "relative_humidity_2m", "apparent_temperature", "is_day", "weather_code",
		"pressure_msl", "wind_speed_10m", "wind_direction_10m", "wind_gusts_10m", "visibility", "dew_point_2m", "uv_index")
	omHourlyFields = joinFields("temperature_2m", "apparent_temperature", "weather_code", "is_day", "cloud_cover",
		"precipitation_probability", "precipitation", "rain", "showers", "snowfall", "pressure_msl", "visibility",
		"wind_speed_10m", "wind_direction_10m", "wind_gusts_10m")
	omDailyFields = joinFields("temperature_2m_max", "temperature_2m_min", "sunrise", "sunset", "precipitation_sum", "uv_index_max")
)

// omForecast is the Open-Meteo forecast response, requested in metric units with unix timestamps.
// Values Open-Meteo leaves null (e.g. past precipitation_probability) decode as zero.
type omForecast struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
	Current   struct {
		Time                int64   `json:"time"`
		Temperature         float64 `json:"temperature_2m"`
		RelativeHumidity    float64 `json:"relative_humidity_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		IsDay               int     `json:"is_day"`
		WeatherCode         int     `json:"weather_code"`
		PressureMSL         float64 `json:"pressure_msl"`
		WindSpeed           float64 `json:"wind_speed_10m"`
		WindDirection       float64 `json:"wind_direction_10m"`
		WindGusts           float64 `json:"wind_gusts_10m"`
		Visibility          float64 `json:"visibility"`
		DewPoint            float64 `json:"dew_point_2m"`
		UVIndex             float64 `json:"uv_index"`
	} `json:"current"`
	Hourly struct {
		Time                     []int64   `json:"time"`
		Temperature              []float64 `json:"temperature_2m"`
		ApparentTemperature      []float64 `json:"apparent_temperature"`
		WeatherCode              []int     `json:"weather_code"`
		IsDay                    []int     `json:"is_day"`
		CloudCover               []float64 `json:"cloud_cover"`
		PrecipitationProbability []float64 `json:"precipitation_probability"`
		Precipitation            []float64 `json:"precipitation"`
		Rain                     []float64 `json:"rain"`
		Showers                  []float64 `json:"showers"`
		Snowfall                 []float64 `json:"snowfall"` // cm
		PressureMSL              []float64 `json:"pressure_msl"`
		Visibility               []float64 `json:"visibility"`
		WindSpeed                []float64 `json:"wind_speed_10m"`
		WindDirection            []float64 `json:"wind_direction_10m"`
		WindGusts                []float64 `json:"wind_gusts_10m"`
	} `json:"hourly"`
	Daily struct {
		Time             []int64   `json:"time"`
		TemperatureMax   []float64 `json:"temperature_2m_max"`
		TemperatureMin   []float64 `json:"temperature_2m_min"`
		Sunrise          []int64   `json:"sunrise"`
		Sunset           []int64   `json:"sunset"`
		PrecipitationSum []float64 `json:"precipitation_sum"`
		UVIndexMax       []float64 `json:"uv_index_max"`
	} `json:"daily"`

	// location is the IANA zone named by Timezone, resolved once when the response is fetched.
	location *time.Location
}
