package app

import (
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestPoolsForVantageSkipsOnlyKnownRemoteContinents(t *testing.T) {
	pools := []model.Pool{
		{ID: "regional", Endpoints: []model.Endpoint{
			{Host: "us.example", Region: "United States"},
			{Host: "eu.example", Region: "Europe"},
			{Host: "asia.example", Continent: "asia"},
		}},
		{ID: "global", Endpoints: []model.Endpoint{{Host: "global.example"}}},
		{ID: "remote-only", Endpoints: []model.Endpoint{{Host: "eu-only.example", Region: "Germany"}}},
	}

	selected := poolsForVantage(pools, "us-west")
	if len(selected) != 2 || len(selected[0].Endpoints) != 1 || selected[0].Endpoints[0].Host != "us.example" || selected[1].ID != "global" {
		t.Fatalf("US selection = %+v", selected)
	}
	unknown := poolsForVantage(pools, "unknown")
	if len(unknown) != len(pools) || len(unknown[0].Endpoints) != 3 {
		t.Fatalf("unknown vantage unexpectedly filtered: %+v", unknown)
	}
}
