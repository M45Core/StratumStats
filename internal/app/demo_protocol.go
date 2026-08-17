package app

import (
	"math/rand"
	"net"
	"strconv"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func appendDemoProtocolData(out []model.Observation, pools []model.Pool, rng *rand.Rand, now time.Time) []model.Observation {
	demoVantages := productionVantages()
	for sample := 0; sample < 24; sample++ {
		observedAt := now.Add(-time.Duration(24-sample) * 6 * time.Hour)
		vantage := demoVantages[sample%len(demoVantages)]
		for index, pool := range pools {
			if len(pool.Endpoints) == 0 {
				continue
			}
			endpoint := pool.Endpoints[0]
			address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
			base := float64(18 + index*4)
			out = append(out,
				demoProtocolObservation(observedAt, vantage, pool.ID, address, endpoint.TLS, model.ProtocolConnect, model.ProtocolStatusOK, base+rng.Float64()*base*.35),
				demoProtocolObservation(observedAt, vantage, pool.ID, address, endpoint.TLS, model.ProtocolSubscribe, model.ProtocolStatusOK, base*1.25+rng.Float64()*base*.5),
				demoProtocolObservation(observedAt, vantage, pool.ID, address, endpoint.TLS, model.ProtocolAuthorize, model.ProtocolStatusOK, base*1.1+rng.Float64()*base*.45),
			)
			if endpoint.TLS {
				out = append(out, demoProtocolObservation(observedAt, vantage, pool.ID, address, true, model.ProtocolTLSHandshake, model.ProtocolStatusOK, base*1.8+rng.Float64()*base*.7))
			}
		}
	}
	return out
}

func demoProtocolObservation(observedAt time.Time, vantage, poolID, endpoint string, tls bool, method, status string, duration float64) model.Observation {
	return model.Observation{
		Version:        model.ObservationVersion,
		RecordType:     model.RecordTypeProtocol,
		ObservedAt:     observedAt,
		Vantage:        vantage,
		PoolID:         poolID,
		Endpoint:       endpoint,
		ProtocolMethod: method,
		DurationMS:     &duration,
		ResponseStatus: status,
		TLS:            tls,
	}
}
