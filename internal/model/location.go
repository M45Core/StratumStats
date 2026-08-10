package model

import "strings"

// EndpointContinent returns the endpoint's coarse location. An explicitly
// recorded continent wins; known registry-region labels are mapped for older
// registry records. An empty result means global or not confidently located.
func EndpointContinent(endpoint Endpoint) string {
	if endpoint.Continent != "" {
		return normalizeContinent(endpoint.Continent)
	}
	return continentForLabel(endpoint.Region)
}

// VantageContinent maps the public probe labels to coarse continents. Unknown
// labels deliberately return empty so collection remains complete by default.
func VantageContinent(vantage string) string {
	label := normalizeLocation(vantage)
	switch label {
	case "us west", "us central", "us east", "us", "united states", "north america", "canada":
		return "north-america"
	case "europe":
		return "europe"
	case "asia", "asia pacific":
		return "asia"
	case "africa":
		return "africa"
	case "south america":
		return "south-america"
	default:
		return ""
	}
}

func continentForLabel(value string) string {
	switch normalizeLocation(value) {
	case "asia", "asia pacific", "singapore", "tokyo japan", "tel aviv israel":
		return "asia"
	case "europe", "france", "frankfurt germany", "germany", "london united kingdom", "poland", "united kingdom":
		return "europe"
	case "atlanta us", "canada", "north america", "seattle us", "united states":
		return "north-america"
	case "johannesburg south africa":
		return "africa"
	case "sao paulo brazil":
		return "south-america"
	default:
		return ""
	}
}

func normalizeContinent(value string) string {
	switch normalizeLocation(value) {
	case "north america":
		return "north-america"
	case "south america":
		return "south-america"
	case "asia", "europe", "africa", "oceania", "antarctica":
		return normalizeLocation(value)
	default:
		return ""
	}
}

func normalizeLocation(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, ",", " ")
	return strings.Join(strings.Fields(value), " ")
}
