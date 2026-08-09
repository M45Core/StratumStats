package app

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/proofofmike/stratumstats/internal/ingest"
	"github.com/proofofmike/stratumstats/internal/model"
	"github.com/proofofmike/stratumstats/internal/probe"
	"github.com/proofofmike/stratumstats/internal/store"
	webapp "github.com/proofofmike/stratumstats/internal/web"
)

func Main(args []string) {
	if err := Run(args); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func Run(args []string) error {
	if len(args) == 0 {
		return serve(nil, true)
	}
	switch args[0] {
	case "serve":
		return serve(args[1:], false)
	case "demo":
		return serve(args[1:], true)
	case "collect":
		return collect(args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(args []string, demo bool) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "HTTP listen address")
	configPath := fs.String("config", "config/pools.json", "pool configuration")
	dataPath := fs.String("data", "data/observations.jsonl", "JSONL observations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	loader := func() ([]model.Observation, error) {
		if demo {
			return demoData(cfg.Pools), nil
		}
		return store.Load(*dataPath)
	}
	var ingestHandler http.Handler
	keyID, secret := os.Getenv("STRATUMSTATS_INGEST_KEY_ID"), os.Getenv("STRATUMSTATS_INGEST_SECRET")
	if (keyID == "") != (secret == "") {
		return fmt.Errorf("STRATUMSTATS_INGEST_KEY_ID and STRATUMSTATS_INGEST_SECRET must be set together")
	}
	if secret != "" && len(secret) < 32 {
		return fmt.Errorf("STRATUMSTATS_INGEST_SECRET must contain at least 32 bytes")
	}
	if !demo && keyID != "" {
		appender := &store.Appender{Path: *dataPath}
		ingestHandler = ingest.Receiver{
			Pools:   cfg.Pools,
			Keys:    map[string][]byte{keyID: []byte(secret)},
			Append:  appender.Append,
			Replays: ingest.NewReplayGuard(),
		}
		log.Printf("authenticated regional-probe ingestion enabled")
	}
	app, err := (webapp.Server{Pools: cfg.Pools, Load: loader, Demo: demo, Ingest: ingestHandler}).Handler()
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           app,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	displayAddr := *addr
	if strings.HasPrefix(displayAddr, ":") {
		displayAddr = "localhost" + displayAddr
	}
	log.Printf("StratumStats listening on http://%s", displayAddr)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func collect(args []string) error {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	configPath := fs.String("config", "config/pools.json", "pool configuration")
	dataPath := fs.String("data", "data/observations.jsonl", "append-only JSONL output")
	vantage := fs.String("vantage", "unknown", "coarse public region, e.g. us-west")
	duration := fs.Duration("duration", 0, "stop after this duration (0 runs until interrupted)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *vantage == "" {
		return fmt.Errorf("vantage must not be empty")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	log.Printf("probing %d pools from vantage %q; credentials rotate and are not stored", len(cfg.Pools), *vantage)
	runID, err := randomRunID()
	if err != nil {
		return err
	}
	var sequence uint64
	appender := &store.Appender{Path: *dataPath}
	return probe.Collect(ctx, cfg.Pools, *vantage, func(batch []model.Observation) error {
		for index := range batch {
			sequence++
			batch[index].Source = "local"
			batch[index].RunID = runID
			batch[index].ObservationID = fmt.Sprintf("%s/%d", runID, sequence)
		}
		log.Printf("writing %d telemetry records", len(batch))
		return appender.Append(batch)
	})
}

func randomRunID() (string, error) {
	var value [16]byte
	if _, err := cryptorand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate collector run id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func loadConfig(path string) (model.Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return model.Config{}, err
	}
	var cfg model.Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	seen := map[string]bool{}
	for _, p := range cfg.Pools {
		if p.ID == "" || p.Name == "" {
			return cfg, fmt.Errorf("every pool needs id and name")
		}
		if seen[p.ID] {
			return cfg, fmt.Errorf("duplicate pool id %q", p.ID)
		}
		seen[p.ID] = true
		if p.Website != "" && !validWebURL(p.Website) {
			return cfg, fmt.Errorf("invalid website for %s", p.ID)
		}
		if p.Category != "" && !oneOf(p.Category, "solo", "shared", "hybrid", "decentralized") {
			return cfg, fmt.Errorf("invalid category %q for %s", p.Category, p.ID)
		}
		if p.Status != "" && !oneOf(p.Status, "active", "inactive", "unverified") {
			return cfg, fmt.Errorf("invalid status %q for %s", p.Status, p.ID)
		}
		if p.ProbeStatus != "" && !oneOf(p.ProbeStatus, "compatible", "requires_credentials", "no_central_endpoint", "unverified") {
			return cfg, fmt.Errorf("invalid probe status %q for %s", p.ProbeStatus, p.ID)
		}
		if p.Status != "" {
			if _, err := time.Parse("2006-01-02", p.LastVerified); err != nil {
				return cfg, fmt.Errorf("invalid last_verified for %s", p.ID)
			}
		}
		if p.AdvertisedFee != "" {
			if _, err := time.Parse("2006-01-02", p.FeeCheckedAt); err != nil {
				return cfg, fmt.Errorf("invalid fee_checked_at for %s", p.ID)
			}
		}
		for _, source := range p.ResearchSources {
			if source.Title == "" || !validWebURL(source.URL) {
				return cfg, fmt.Errorf("invalid research source for %s", p.ID)
			}
		}
		endpointSeen := map[string]bool{}
		for _, e := range p.Endpoints {
			if e.Host == "" || e.Port < 1 || e.Port > 65535 {
				return cfg, fmt.Errorf("invalid endpoint for %s", p.ID)
			}
			key := fmt.Sprintf("%s:%d:%t", e.Host, e.Port, e.TLS)
			if endpointSeen[key] {
				return cfg, fmt.Errorf("duplicate endpoint %s for %s", key, p.ID)
			}
			endpointSeen[key] = true
		}
	}
	return cfg, nil
}

func validWebURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func demoData(pools []model.Pool) []model.Observation {
	rng := rand.New(rand.NewSource(81))
	out := make([]model.Observation, 0, len(pools)*120)
	now := time.Now().UTC()
	for i := 0; i < 120; i++ {
		for p, pool := range pools {
			base := float64(55 + p*75)
			arrived := rng.Float64() > float64(p+1)*.012
			offset := base + rng.Float64()*base*1.8
			observation := model.Observation{Version: model.ObservationVersion, ObservedAt: now.Add(-time.Duration(120-i) * 10 * time.Minute), Vantage: "demo", BlockID: fmt.Sprintf("demo-%03d", i), PoolID: pool.ID, Eligible: true, Arrived: arrived, OffsetMS: offset, EmptyFirst: rng.Float64() < float64(p)*.018, TLS: p%2 == 0, CoinbaseAnalyzed: arrived}
			if pool.Category == "solo" && arrived {
				fee := float64((p % 4)) * 0.5
				total := uint64(312_500_000)
				poolShare := uint64(float64(total) * fee / 100)
				observation.WorkerWalletInCoinbase = true
				observation.CoinbaseTotalSats = total
				observation.WorkerPayoutSats = total - poolShare
				observation.EstimatedPoolFeePct = &fee
				observation.CoinbaseOutputs = nil
				observation.CoinbaseOutputCount = 2 // private worker destination plus a zero-value commitment
				if poolShare > 0 {
					observation.CoinbaseOutputs = append(observation.CoinbaseOutputs, model.CoinbaseOutput{ValueSats: poolShare, ScriptPubKey: "0014751e76e8199196d454941c45d1b3a323f1433bd6", Address: "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4", ScriptType: "p2wpkh"})
					observation.CoinbaseOutputCount++
				}
			} else if arrived {
				total := uint64(312_500_000)
				first, second := total*60/100, total*25/100
				observation.CoinbaseTotalSats = total
				observation.CoinbaseOutputs = []model.CoinbaseOutput{
					{ValueSats: first, ScriptPubKey: "0014751e76e8199196d454941c45d1b3a323f1433bd6", Address: "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4", ScriptType: "p2wpkh"},
					{ValueSats: second, ScriptPubKey: "512079be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798", Address: "bc1p0xlxvlhemja6c4dqv22uapctqupfhlxm9h8z3k2e72q4k9hcz7vqzk5jj0", ScriptType: "p2tr"},
					{ValueSats: total - first - second, ScriptPubKey: "a914111111111111111111111111111111111111111187", ScriptType: "p2sh"},
				}
				observation.CoinbaseOutputCount = len(observation.CoinbaseOutputs) + 1
			}
			out = append(out, observation)
		}
	}
	return appendDemoProtocolData(out, pools, rng, now)
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: stratumstats [serve|demo|collect] [options]")
	fmt.Fprintln(os.Stderr, "No command starts the synthetic demo dashboard on :8080.")
}
