package model

import "encoding/json"

// UnmarshalJSON keeps version 1-5 JSONL readable after later schema additions.
// Version 3 replaced contest-oriented names with block/report terminology.
func (o *Observation) UnmarshalJSON(data []byte) error {
	type observationAlias Observation
	aux := struct {
		*observationAlias
		LegacyBlockID string   `json:"race_id"`
		LegacyFeePct  *float64 `json:"estimated_coinbase_deduction_pct"`
	}{observationAlias: (*observationAlias)(o)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if o.BlockID == "" {
		o.BlockID = aux.LegacyBlockID
	}
	if o.EstimatedPoolFeePct == nil {
		o.EstimatedPoolFeePct = aux.LegacyFeePct
	}
	return nil
}
