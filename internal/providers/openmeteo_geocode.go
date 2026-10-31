package providers

import "strings"

type geocodeResult struct {
	Name        string  `json:"name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Admin1      string  `json:"admin1"`
}

// splitLocationQuery splits a config location like "Saskatoon, SK, Canada" into the place name
// Open-Meteo searches on and the qualifiers used to choose among same-named places.
func splitLocationQuery(location string) (string, []string) {
	parts := strings.Split(location, ",")
	qualifiers := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			qualifiers = append(qualifiers, trimmed)
		}
	}
	return strings.TrimSpace(parts[0]), qualifiers
}

// bestGeocodeMatch returns the result matching the most qualifiers. With no qualifiers it takes
// Open-Meteo's top (most populous) result; with qualifiers, at least one must match so a bare
// name never silently resolves to a same-named town elsewhere.
func bestGeocodeMatch(results []geocodeResult, qualifiers []string) (geocodeResult, bool) {
	if len(results) == 0 {
		return geocodeResult{}, false
	}
	if len(qualifiers) == 0 {
		return results[0], true
	}
	best, bestScore := geocodeResult{}, 0
	for _, result := range results {
		score := 0
		for _, qualifier := range qualifiers {
			if result.matches(qualifier) {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = result, score
		}
	}
	return best, bestScore > 0
}

// matches reports whether a qualifier names this result's country or first-level region,
// by full name, ISO country code, or US/Canadian postal abbreviation.
func (r geocodeResult) matches(qualifier string) bool {
	upper := strings.ToUpper(qualifier)
	if alias, ok := countryAliases[upper]; ok {
		upper = alias
	}
	return strings.EqualFold(qualifier, r.Country) ||
		strings.EqualFold(qualifier, r.Admin1) ||
		upper == r.CountryCode ||
		strings.EqualFold(regionAbbreviations[upper], r.Admin1)
}

var countryAliases = map[string]string{"USA": "US", "UK": "GB"}

// regionAbbreviations expands US state and Canadian province postal codes; none collide.
var regionAbbreviations = map[string]string{
	"AB": "Alberta", "BC": "British Columbia", "MB": "Manitoba", "NB": "New Brunswick",
	"NL": "Newfoundland and Labrador", "NS": "Nova Scotia", "NT": "Northwest Territories", "NU": "Nunavut",
	"ON": "Ontario", "PE": "Prince Edward Island", "QC": "Quebec", "SK": "Saskatchewan", "YT": "Yukon",

	"AL": "Alabama", "AK": "Alaska", "AZ": "Arizona", "AR": "Arkansas", "CA": "California",
	"CO": "Colorado", "CT": "Connecticut", "DE": "Delaware", "FL": "Florida",
	"GA": "Georgia", "HI": "Hawaii", "ID": "Idaho", "IL": "Illinois", "IN": "Indiana",
	"IA": "Iowa", "KS": "Kansas", "KY": "Kentucky", "LA": "Louisiana", "ME": "Maine",
	"MD": "Maryland", "MA": "Massachusetts", "MI": "Michigan", "MN": "Minnesota", "MS": "Mississippi",
	"MO": "Missouri", "MT": "Montana", "NE": "Nebraska", "NV": "Nevada", "NH": "New Hampshire",
	"NJ": "New Jersey", "NM": "New Mexico", "NY": "New York", "NC": "North Carolina", "ND": "North Dakota",
	"OH": "Ohio", "OK": "Oklahoma", "OR": "Oregon", "PA": "Pennsylvania", "RI": "Rhode Island",
	"SC": "South Carolina", "SD": "South Dakota", "TN": "Tennessee", "TX": "Texas", "UT": "Utah",
	"VT": "Vermont", "VA": "Virginia", "WA": "Washington", "WV": "West Virginia", "WI": "Wisconsin",
	"WY": "Wyoming",
}
