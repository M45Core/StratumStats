package model

import "testing"

func TestEndpointContinentUsesExplicitValueThenKnownRegion(t *testing.T) {
	for _, test := range []struct {
		endpoint Endpoint
		want     string
	}{
		{Endpoint{Continent: "North America", Region: "Asia"}, "north-america"},
		{Endpoint{Region: "Frankfurt, Germany"}, "europe"},
		{Endpoint{Region: "Tel Aviv, Israel"}, "asia"},
		{Endpoint{Region: "global"}, ""},
	} {
		if got := EndpointContinent(test.endpoint); got != test.want {
			t.Errorf("EndpointContinent(%+v) = %q, want %q", test.endpoint, got, test.want)
		}
	}
}

func TestVantageContinent(t *testing.T) {
	for _, test := range []struct{ vantage, want string }{
		{"us-west", "north-america"}, {"europe", "europe"}, {"unknown", ""},
	} {
		if got := VantageContinent(test.vantage); got != test.want {
			t.Errorf("VantageContinent(%q) = %q, want %q", test.vantage, got, test.want)
		}
	}
}
