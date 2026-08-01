package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

const blockWindow = 15 * time.Second

type event struct {
	poolID, prevHash    string
	at                  time.Time
	full, tls           bool
	verified            bool
	coinbaseAnalyzed    bool
	workerWalletSeen    bool
	coinbaseTotalSats   uint64
	workerPayoutSats    uint64
	estimatedPoolFeePct *float64
	connected           *bool
}

type activeBlock struct {
	id       string
	started  time.Time
	eligible map[string]bool
	arrivals map[string]time.Time
	empty    map[string]bool
	tls      map[string]bool
	invalid  map[string]bool
	payout   map[string]event
}

// Collect connects to every configured endpoint and emits completed block
// blocks. It submits no shares and never stores the randomized credentials.
func Collect(ctx context.Context, pools []model.Pool, vantage string, emit func([]model.Observation) error) error {
	events := make(chan event, 256)
	var wg sync.WaitGroup
	for _, p := range pools {
		for _, endpoint := range p.Endpoints {
			p, endpoint := p, endpoint
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := watch(ctx, p.ID, endpoint, events); err != nil && ctx.Err() == nil {
					log.Printf("probe %s %s:%d: %v", p.ID, endpoint.Host, endpoint.Port, err)
				}
			}()
		}
	}
	go func() { wg.Wait(); close(events) }()

	connected := map[string]bool{}
	blocks := map[string]*activeBlock{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	finish := func(r *activeBlock) error {
		if len(r.arrivals) < 2 {
			return nil
		}
		var first time.Time
		for _, at := range r.arrivals {
			if first.IsZero() || at.Before(first) {
				first = at
			}
		}
		ids := make([]string, 0, len(r.eligible))
		for id := range r.eligible {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		out := make([]model.Observation, 0, len(ids))
		for _, id := range ids {
			at, ok := r.arrivals[id]
			o := model.Observation{Version: model.ObservationVersion, ObservedAt: r.started.UTC(), Vantage: vantage, BlockID: r.id, PoolID: id, Eligible: true, Arrived: ok, EmptyFirst: r.empty[id], TLS: r.tls[id]}
			payout := r.payout[id]
			o.CoinbaseAnalyzed = payout.coinbaseAnalyzed
			o.WorkerWalletInCoinbase = payout.workerWalletSeen
			o.CoinbaseTotalSats = payout.coinbaseTotalSats
			o.WorkerPayoutSats = payout.workerPayoutSats
			o.EstimatedPoolFeePct = payout.estimatedPoolFeePct
			if r.invalid[id] {
				o.ErrorCategory = "invalid_job"
			}
			if ok {
				o.OffsetMS = float64(at.Sub(first).Microseconds()) / 1000
			}
			out = append(out, o)
		}
		return emit(out)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e, ok := <-events:
			if !ok {
				return nil
			}
			if e.connected != nil {
				connected[e.poolID] = *e.connected
				continue
			}
			r := blocks[e.prevHash]
			if r == nil {
				r = &activeBlock{id: e.prevHash, started: e.at, eligible: map[string]bool{}, arrivals: map[string]time.Time{}, empty: map[string]bool{}, tls: map[string]bool{}, invalid: map[string]bool{}, payout: map[string]event{}}
				for id, online := range connected {
					if online {
						r.eligible[id] = true
					}
				}
				r.eligible[e.poolID] = true
				blocks[e.prevHash] = r
			}
			if !e.full {
				r.empty[e.poolID] = true
				continue
			}
			if !e.verified {
				r.invalid[e.poolID] = true
				continue
			}
			if old, exists := r.arrivals[e.poolID]; !exists || e.at.Before(old) {
				r.arrivals[e.poolID], r.tls[e.poolID] = e.at, e.tls
				r.payout[e.poolID] = e
			}
		case now := <-ticker.C:
			for id, r := range blocks {
				if now.Sub(r.started) >= blockWindow {
					if err := finish(r); err != nil {
						return err
					}
					delete(blocks, id)
				}
			}
		}
	}
}

func watchSession(ctx context.Context, poolID string, endpoint model.Endpoint, out chan<- event) error {
	identity, err := RandomIdentity()
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
	if endpoint.TLS {
		conn, err = tls.DialWithDialer(&dialer, "tcp", address, &tls.Config{ServerName: endpoint.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	online := true
	select {
	case out <- event{poolID: poolID, connected: &online}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() {
		offline := false
		select {
		case out <- event{poolID: poolID, connected: &offline}:
		case <-ctx.Done():
		}
	}()

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	if err := request(w, 1, "mining.subscribe", []string{identity.Agent}); err != nil {
		return err
	}
	subscribeResult, err := awaitResponse(ctx, conn, r, 1)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	var subscribe []json.RawMessage
	var extraNonce1 string
	var extraNonce2Size int
	if json.Unmarshal(subscribeResult, &subscribe) == nil && len(subscribe) >= 3 {
		_ = json.Unmarshal(subscribe[1], &extraNonce1)
		_ = json.Unmarshal(subscribe[2], &extraNonce2Size)
	}
	if err := request(w, 2, "mining.authorize", []string{identity.Username, "x"}); err != nil {
		return err
	}
	if _, err := awaitResponse(ctx, conn, r, 2); err != nil {
		return fmt.Errorf("authorize: %w", err)
	}

	var previous string
	var fullSent bool
	for {
		if err := conn.SetReadDeadline(time.Now().Add(90 * time.Second)); err != nil {
			return err
		}
		line, err := r.ReadBytes('\n')
		if err != nil {
			return err
		}
		var msg struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if json.Unmarshal(line, &msg) != nil || msg.Method != "mining.notify" || len(msg.Params) < 9 {
			continue
		}
		prev, ok := msg.Params[1].(string)
		if !ok || prev == "" {
			continue
		}
		clean, _ := msg.Params[8].(bool)
		branches, _ := msg.Params[4].([]any)
		branchStrings := make([]string, 0, len(branches))
		for _, branch := range branches {
			if value, ok := branch.(string); ok {
				branchStrings = append(branchStrings, value)
			}
		}
		if previous == "" {
			previous = prev
			continue
		}
		if prev != previous {
			if !clean {
				continue
			}
			previous, fullSent = prev, false
		}
		if len(branches) > 0 && fullSent {
			continue
		}
		job := Job{PrevHash: prev, MerkleBranches: branchStrings, ExtraNonce1: extraNonce1, ExtraNonce2Size: extraNonce2Size, WorkerScript: identity.PayoutScript}
		job.Coinbase1, _ = msg.Params[2].(string)
		job.Coinbase2, _ = msg.Params[3].(string)
		job.Version, _ = msg.Params[5].(string)
		job.Bits, _ = msg.Params[6].(string)
		job.NTime, _ = msg.Params[7].(string)
		verification := VerifyJob(job)
		if len(branches) > 0 {
			fullSent = true
		}
		e := event{poolID: poolID, prevHash: prev, at: time.Now(), full: len(branches) > 0, tls: endpoint.TLS, verified: verification.Valid, coinbaseAnalyzed: verification.CoinbaseAnalyzed, workerWalletSeen: verification.WorkerWalletSeen, coinbaseTotalSats: verification.CoinbaseTotalSats, workerPayoutSats: verification.WorkerPayoutSats, estimatedPoolFeePct: verification.EstimatedPoolFeePct}
		select {
		case out <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func request(w *bufio.Writer, id int, method string, params any) error {
	b, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return w.Flush()
}

func awaitResponse(ctx context.Context, conn net.Conn, r *bufio.Reader, id int) (json.RawMessage, error) {
	for {
		if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			return nil, err
		}
		line, err := r.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var msg struct {
			ID     any             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  any             `json:"error"`
		}
		if json.Unmarshal(line, &msg) != nil || msg.ID == nil {
			continue
		}
		got := -1
		switch v := msg.ID.(type) {
		case float64:
			got = int(v)
		case string:
			got, _ = strconv.Atoi(v)
		}
		if got != id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("remote error: %v", msg.Error)
		}
		if id == 2 {
			var accepted bool
			if json.Unmarshal(msg.Result, &accepted) == nil && !accepted {
				return nil, fmt.Errorf("authorization rejected")
			}
		}
		return msg.Result, nil
	}
}

var _ io.Reader
