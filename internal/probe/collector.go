package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

const blockWindow = 15 * time.Second

var (
	requestTimeout     = 30 * time.Second
	pingInterval       = 60 * time.Second
	pingResponseWindow = 10 * time.Second
	sessionReadTimeout = 90 * time.Second
)

type event struct {
	poolID, prevHash     string
	at                   time.Time
	hasTransactions, tls bool
	verified             bool
	coinbaseAnalyzed     bool
	workerWalletSeen     bool
	coinbaseTotalSats    uint64
	workerPayoutSats     uint64
	estimatedPoolFeePct  *float64
	connected            *bool
	protocol             *model.Observation
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

// Collect connects to every configured endpoint and emits block and protocol
// observations. It submits no shares and never stores randomized credentials.
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
			if e.protocol != nil {
				record := *e.protocol
				record.Vantage = vantage
				if err := emit([]model.Observation{record}); err != nil {
					return err
				}
				continue
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
			recordBlockEvent(r, e)
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
	address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))

	connectStarted := time.Now()
	rawConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolConnect, connectStarted, protocolErrorStatus(err), "connect_failed")
		return err
	}
	if err := publishProtocol(ctx, out, poolID, endpoint, model.ProtocolConnect, connectStarted, model.ProtocolStatusOK, ""); err != nil {
		rawConn.Close()
		return err
	}
	defer rawConn.Close()

	var conn net.Conn = rawConn
	if endpoint.TLS {
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: endpoint.Host, MinVersion: tls.VersionTLS12})
		tlsStarted := time.Now()
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolTLSHandshake, tlsStarted, protocolErrorStatus(err), "tls_handshake_failed")
			return err
		}
		if err := publishProtocol(ctx, out, poolID, endpoint, model.ProtocolTLSHandshake, tlsStarted, model.ProtocolStatusOK, ""); err != nil {
			return err
		}
		conn = tlsConn
	}

	closeOnCancelDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-closeOnCancelDone:
		}
	}()
	defer close(closeOnCancelDone)

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	subscribeStarted := time.Now()
	if err := request(w, 1, "mining.subscribe", []string{identity.Agent}); err != nil {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolSubscribe, subscribeStarted, model.ProtocolStatusError, "subscribe_write_failed")
		return err
	}
	subscribeResult, remoteErr, err := awaitResponse(ctx, conn, r, w, identity.Agent, 1, requestTimeout)
	if err != nil {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolSubscribe, subscribeStarted, protocolErrorStatus(err), "subscribe_response_failed")
		return fmt.Errorf("subscribe: %w", err)
	}
	if remoteErr != nil {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolSubscribe, subscribeStarted, model.ProtocolStatusRejected, "subscribe_rejected")
		return fmt.Errorf("subscribe rejected: %v", remoteErr)
	}
	var subscribe []json.RawMessage
	var extraNonce1 string
	var extraNonce2Size int
	if json.Unmarshal(subscribeResult, &subscribe) != nil || len(subscribe) < 3 {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolSubscribe, subscribeStarted, model.ProtocolStatusError, "subscribe_invalid_response")
		return fmt.Errorf("subscribe: invalid response")
	}
	if json.Unmarshal(subscribe[1], &extraNonce1) != nil || json.Unmarshal(subscribe[2], &extraNonce2Size) != nil || extraNonce1 == "" || extraNonce2Size <= 0 {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolSubscribe, subscribeStarted, model.ProtocolStatusError, "subscribe_invalid_extranonce")
		return fmt.Errorf("subscribe: invalid extranonce")
	}
	if err := publishProtocol(ctx, out, poolID, endpoint, model.ProtocolSubscribe, subscribeStarted, model.ProtocolStatusOK, ""); err != nil {
		return err
	}

	authorizeStarted := time.Now()
	if err := request(w, 2, "mining.authorize", []string{identity.Username, "x"}); err != nil {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolAuthorize, authorizeStarted, model.ProtocolStatusError, "authorize_write_failed")
		return err
	}
	authorizeResult, remoteErr, err := awaitResponse(ctx, conn, r, w, identity.Agent, 2, requestTimeout)
	if err != nil {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolAuthorize, authorizeStarted, protocolErrorStatus(err), "authorize_response_failed")
		return fmt.Errorf("authorize: %w", err)
	}
	if remoteErr != nil {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolAuthorize, authorizeStarted, model.ProtocolStatusRejected, "authorize_rejected")
		return fmt.Errorf("authorize rejected: %v", remoteErr)
	}
	var authorized bool
	if json.Unmarshal(authorizeResult, &authorized) != nil || !authorized {
		_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolAuthorize, authorizeStarted, model.ProtocolStatusRejected, "authorize_rejected")
		return fmt.Errorf("authorization rejected")
	}
	if err := publishProtocol(ctx, out, poolID, endpoint, model.ProtocolAuthorize, authorizeStarted, model.ProtocolStatusOK, ""); err != nil {
		return err
	}

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

	var previous string
	var transactionJobSent bool
	pingID := 1000
	pingPending := false
	pingDisabled := false
	pingStarted := time.Time{}
	pingDeadline := time.Time{}
	nextPing := time.Now()

	for {
		now := time.Now()
		if !pingDisabled && !pingPending && !nextPing.IsZero() && !now.Before(nextPing) {
			pingID++
			pingStarted = now
			if err := request(w, pingID, model.ProtocolPing, []any{}); err != nil {
				_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolPing, pingStarted, model.ProtocolStatusError, "ping_write_failed")
				return err
			}
			pingPending = true
			pingDeadline = now.Add(pingResponseWindow)
		}

		readDeadline := now.Add(sessionReadTimeout)
		if pingPending && pingDeadline.Before(readDeadline) {
			readDeadline = pingDeadline
		}
		if !pingDisabled && !pingPending && !nextPing.IsZero() && nextPing.Before(readDeadline) {
			readDeadline = nextPing
		}
		if err := conn.SetReadDeadline(readDeadline); err != nil {
			return err
		}
		line, err := r.ReadBytes('\n')
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			now = time.Now()
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if pingPending && !now.Before(pingDeadline) {
					if publishErr := publishProtocol(ctx, out, poolID, endpoint, model.ProtocolPing, pingStarted, model.ProtocolStatusTimeout, "ping_timeout"); publishErr != nil {
						return publishErr
					}
					pingPending, pingDisabled = false, true
					continue
				}
				if !pingDisabled && !pingPending && !nextPing.IsZero() && !now.Before(nextPing) {
					continue
				}
			}
			if pingPending {
				_ = publishProtocol(ctx, out, poolID, endpoint, model.ProtocolPing, pingStarted, model.ProtocolStatusError, "ping_connection_closed")
			}
			return err
		}

		var msg struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params []any           `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  any             `json:"error"`
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		if pingPending && responseID(msg.ID) == pingID {
			status, category := model.ProtocolStatusOK, ""
			if msg.Error != nil {
				status, category = model.ProtocolStatusUnsupported, "ping_unsupported"
				pingDisabled = true
			} else {
				var pong string
				if json.Unmarshal(msg.Result, &pong) != nil || !strings.EqualFold(pong, "pong") {
					status, category = model.ProtocolStatusError, "ping_invalid_response"
					pingDisabled = true
				}
			}
			if err := publishProtocol(ctx, out, poolID, endpoint, model.ProtocolPing, pingStarted, status, category); err != nil {
				return err
			}
			pingPending = false
			if !pingDisabled {
				nextPing = time.Now().Add(pingInterval)
			}
			continue
		}
		if msg.Method == "client.get_version" {
			if err := response(w, msg.ID, identity.Agent); err != nil {
				return err
			}
			continue
		}
		if msg.Method != "mining.notify" || len(msg.Params) < 9 {
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
			previous, transactionJobSent = prev, false
		}
		if len(branches) > 0 && transactionJobSent {
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
			transactionJobSent = true
		}
		e := event{poolID: poolID, prevHash: prev, at: time.Now(), hasTransactions: len(branches) > 0, tls: endpoint.TLS, verified: verification.Valid, coinbaseAnalyzed: verification.CoinbaseAnalyzed, workerWalletSeen: verification.WorkerWalletSeen, coinbaseTotalSats: verification.CoinbaseTotalSats, workerPayoutSats: verification.WorkerPayoutSats, estimatedPoolFeePct: verification.EstimatedPoolFeePct}
		select {
		case out <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// recordBlockEvent keeps the earliest structurally valid template for a pool.
// A coinbase-only template is useful work and counts immediately; the presence
// of transaction branches is retained only as raw empty-first evidence.
func recordBlockEvent(block *activeBlock, e event) {
	if !e.hasTransactions {
		block.empty[e.poolID] = true
	}
	if !e.verified {
		block.invalid[e.poolID] = true
		return
	}
	if old, exists := block.arrivals[e.poolID]; !exists || e.at.Before(old) {
		block.arrivals[e.poolID], block.tls[e.poolID] = e.at, e.tls
		block.payout[e.poolID] = e
	}
}

func publishProtocol(ctx context.Context, out chan<- event, poolID string, endpoint model.Endpoint, method string, started time.Time, status, errorCategory string) error {
	duration := float64(time.Since(started).Nanoseconds()) / float64(time.Millisecond)
	record := model.Observation{
		Version:        model.ObservationVersion,
		RecordType:     model.RecordTypeProtocol,
		ObservedAt:     time.Now().UTC(),
		PoolID:         poolID,
		Endpoint:       net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port)),
		ProtocolMethod: method,
		DurationMS:     &duration,
		ResponseStatus: status,
		TLS:            endpoint.TLS,
		ErrorCategory:  errorCategory,
	}
	select {
	case out <- event{poolID: poolID, protocol: &record}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func protocolErrorStatus(err error) string {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return model.ProtocolStatusTimeout
	}
	return model.ProtocolStatusError
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

func response(w *bufio.Writer, id any, result any) error {
	b, err := json.Marshal(map[string]any{"id": id, "result": result, "error": nil})
	if err != nil {
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return w.Flush()
}

func awaitResponse(ctx context.Context, conn net.Conn, r *bufio.Reader, w *bufio.Writer, agent string, id int, timeout time.Duration) (json.RawMessage, any, error) {
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, nil, err
		}
		line, err := r.ReadBytes('\n')
		if err != nil {
			return nil, nil, err
		}
		var msg struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  any             `json:"error"`
		}
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		if msg.Method == "client.get_version" {
			if err := response(w, msg.ID, agent); err != nil {
				return nil, nil, err
			}
			continue
		}
		if responseID(msg.ID) != id {
			continue
		}
		return msg.Result, msg.Error, nil
	}
}

func responseID(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case string:
		id, _ := strconv.Atoi(v)
		return id
	default:
		return -1
	}
}
