package grok

import (
	"encoding/binary"
	json "encoding/json/v2"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

type productFixture struct {
	kind uint64
	used *float32
}

func authPath(t *testing.T, scope, token, account, team, expires string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	entry := map[string]any{"key": token}
	if account != "" {
		entry["email"] = account
	}
	if team != "" {
		entry["team_id"] = team
	}
	if expires != "" {
		entry["expires_at"] = expires
	}
	body, err := json.Marshal(map[string]any{scope: entry})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testAdapter(t *testing.T, handler http.HandlerFunc, path string, now time.Time) (Adapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(Options{AuthFile: path, Client: server.Client(), Endpoint: server.URL, Timeout: time.Second, Now: func() time.Time { return now }}), server
}

func request() evidence.Request {
	return evidence.Request{Provider: "grok", Profile: "default"}
}

func grpcResponse(payload []byte) []byte {
	data := frame(0, payload)
	return append(data, frame(0x80, []byte("grpc-status: 0\r\n"))...)
}

func frame(flags byte, payload []byte) []byte {
	result := make([]byte, 5, 5+len(payload))
	result[0] = flags
	binary.BigEndian.PutUint32(result[1:], uint32(len(payload)))
	return append(result, payload...)
}

func quotaPayload(shared *float32, products []productFixture, period uint64, start, reset time.Time, prepaid *uint64) []byte {
	var config []byte
	if shared != nil {
		config = append(config, fixed32Field(1, math.Float32bits(*shared))...)
	}
	for _, product := range products {
		var item []byte
		item = append(item, varintField(1, product.kind)...)
		if product.used != nil {
			item = append(item, fixed32Field(2, math.Float32bits(*product.used))...)
		}
		config = append(config, bytesField(7, item)...)
	}
	if period != 0 || !start.IsZero() || !reset.IsZero() {
		var current []byte
		if period != 0 {
			current = append(current, varintField(1, period)...)
		}
		if !start.IsZero() {
			current = append(current, bytesField(2, timestampMessage(start))...)
		}
		if !reset.IsZero() {
			current = append(current, bytesField(3, timestampMessage(reset))...)
		}
		config = append(config, bytesField(8, current)...)
	}
	if prepaid != nil {
		config = append(config, bytesField(12, varintField(1, *prepaid))...)
	}
	return bytesField(1, config)
}

func timestampMessage(value time.Time) []byte {
	result := varintField(1, uint64(value.Unix()))
	if value.Nanosecond() != 0 {
		result = append(result, varintField(2, uint64(value.Nanosecond()))...)
	}
	return result
}

func varintField(number uint64, value uint64) []byte {
	return append(varint(number<<3), varint(value)...)
}

func bytesField(number uint64, value []byte) []byte {
	result := append(varint(number<<3|2), varint(uint64(len(value)))...)
	return append(result, value...)
}

func fixed32Field(number uint64, value uint32) []byte {
	result := varint(number<<3 | 5)
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	return append(result, raw[:]...)
}

func varint(value uint64) []byte {
	var result []byte
	for value >= 0x80 {
		result = append(result, byte(value)|0x80)
		value >>= 7
	}
	return append(result, byte(value))
}

func limit(t *testing.T, observation evidence.Observation, windowID, limitID string) evidence.Limit {
	t.Helper()
	for _, window := range observation.Windows {
		if string(window.ID) != windowID {
			continue
		}
		for _, item := range window.Limits {
			if item.ID == limitID {
				return item
			}
		}
	}
	t.Fatalf("missing limit %s/%s in %+v", windowID, limitID, observation.Windows)
	return evidence.Limit{}
}

func writeQuota(w http.ResponseWriter, payload []byte) {
	w.Header().Set("Content-Type", "application/grpc-web+proto")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(grpcResponse(payload))
}

func assertSourceRequest(t *testing.T, r *http.Request, token string) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if r.Method != http.MethodPost || fmt.Sprint(body) != "[0 0 0 0 0]" {
		t.Errorf("request = %s %v", r.Method, body)
	}
	if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Content-Type") != "application/grpc-web+proto" || r.Header.Get("Origin") != "https://grok.com" || r.Header.Get("Referer") != "https://grok.com/?_s=usage" || r.Header.Get("X-Grpc-Web") != "1" || r.Header.Get("X-User-Agent") != "connect-es/2.1.1" {
		t.Errorf("source headers do not match the Grok consumer RPC")
	}
}
