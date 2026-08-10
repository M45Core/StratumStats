package probe

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestWatchSessionMeasuresProtocolResponses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		readMethod := func(want string) (int, error) {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return 0, err
			}
			var request struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			if err := json.Unmarshal(line, &request); err != nil {
				return 0, err
			}
			if request.Method != want {
				return 0, fmt.Errorf("method=%q want=%q", request.Method, want)
			}
			return request.ID, nil
		}
		respond := func(value any) error {
			if err := json.NewEncoder(w).Encode(value); err != nil {
				return err
			}
			return w.Flush()
		}

		id, err := readMethod(model.ProtocolSubscribe)
		if err != nil {
			serverErr <- err
			return
		}
		time.Sleep(12 * time.Millisecond)
		if err := respond(map[string]any{"id": id, "result": []any{[]any{}, "01020304", 4}, "error": nil}); err != nil {
			serverErr <- err
			return
		}

		id, err = readMethod(model.ProtocolAuthorize)
		if err != nil {
			serverErr <- err
			return
		}
		time.Sleep(8 * time.Millisecond)
		if err := respond(map[string]any{"id": id, "result": true, "error": nil}); err != nil {
			serverErr <- err
			return
		}

		id, err = readMethod(model.ProtocolPing)
		if err != nil {
			serverErr <- err
			return
		}
		time.Sleep(6 * time.Millisecond)
		serverErr <- respond(map[string]any{"id": id, "result": "pong", "error": nil})
	}()

	address := listener.Addr().(*net.TCPAddr)
	out := make(chan event, 16)
	err = watchSession(context.Background(), "test-pool", model.Endpoint{Host: "127.0.0.1", Port: address.Port}, out)
	if err == nil {
		t.Fatal("session should end when the fixture closes its connection")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	close(out)

	records := map[string]model.Observation{}
	for e := range out {
		if e.protocol != nil {
			records[e.protocol.ProtocolMethod] = *e.protocol
		}
	}
	for _, method := range []string{model.ProtocolConnect, model.ProtocolSubscribe, model.ProtocolAuthorize, model.ProtocolPing} {
		record, ok := records[method]
		if !ok {
			t.Errorf("missing %s timing", method)
			continue
		}
		if record.RecordType != model.RecordTypeProtocol || record.ResponseStatus != model.ProtocolStatusOK {
			t.Errorf("%s record=%+v", method, record)
		}
		if record.DurationMS == nil || *record.DurationMS < 0 {
			t.Errorf("%s duration=%v", method, record.DurationMS)
		}
	}
	if got := records[model.ProtocolSubscribe].DurationMS; got == nil || *got < 8 {
		t.Errorf("subscribe timing did not include response delay: %v", got)
	}
	if got := records[model.ProtocolAuthorize].DurationMS; got == nil || *got < 5 {
		t.Errorf("authorize timing did not include response delay: %v", got)
	}
}

func TestWatchSessionReportsInvalidTLSCertificate(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()

	address := server.Listener.Addr().(*net.TCPAddr)
	out := make(chan event, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := watchSession(ctx, "test-pool", model.Endpoint{Host: "127.0.0.1", Port: address.Port, TLS: true}, out); err == nil {
		t.Fatal("session unexpectedly accepted an untrusted certificate")
	}
	close(out)

	var tlsRecord *model.Observation
	for e := range out {
		if e.protocol != nil && e.protocol.ProtocolMethod == model.ProtocolTLSHandshake {
			record := *e.protocol
			tlsRecord = &record
		}
	}
	if tlsRecord == nil {
		t.Fatal("TLS failure observation was not published")
	}
	if tlsRecord.ResponseStatus != model.ProtocolStatusError || tlsRecord.ErrorCategory != model.ProtocolErrorTLSCertificateInvalid {
		t.Fatalf("TLS failure record=%+v", *tlsRecord)
	}
}
