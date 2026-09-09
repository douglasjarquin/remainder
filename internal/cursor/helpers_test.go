package cursor

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

type fixtureResponse struct {
	status   int
	body     string
	bodyFile string
}

func fixtureServer(t *testing.T, responses map[string]fixtureResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := filepath.Base(r.URL.Path)
		response, ok := responses[method]
		if !ok {
			t.Fatalf("unexpected RPC %q", method)
		}
		if response.status != 0 {
			w.WriteHeader(response.status)
		}
		if response.bodyFile != "" {
			fmt.Fprint(w, fixture(t, response.bodyFile))
			return
		}
		fmt.Fprint(w, response.body)
	}))
}

func testAdapter(t *testing.T, server *httptest.Server) Adapter {
	t.Helper()
	endpoint := "http://127.0.0.1:1"
	client := http.DefaultClient
	if server != nil {
		endpoint = server.URL
		client = server.Client()
	}
	return New(Options{AuthFile: writeAuth(t, `synthetic-secret`), Endpoint: endpoint, Client: client, Now: func() time.Time { return fixtureNow }, Timeout: time.Second, goos: "linux"})
}

func cursorRequest() evidence.Request {
	return evidence.Request{Provider: "cursor", Profile: "default"}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func writeAuth(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"accessToken":%q,"refreshToken":"must-not-be-read"}`, token)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertValue(t *testing.T, observation evidence.Observation, window evidence.WindowID, field evidence.Field, want string) {
	t.Helper()
	got, err := evidence.SelectValue(observation, evidence.ValueRequest{Provider: "cursor", Profile: "default", Window: window, Field: field}, evidence.FreshOnly)
	if err != nil || got != want {
		t.Fatalf("%s/%s = %q, %v, want %q", window, field, got, err, want)
	}
}

func windowByID(t *testing.T, observation evidence.Observation, id evidence.WindowID) evidence.Window {
	t.Helper()
	for _, window := range observation.Windows {
		if window.ID == id {
			return window
		}
	}
	t.Fatalf("window %q missing", id)
	return evidence.Window{}
}

func limitByField(t *testing.T, window evidence.Window, field evidence.Field) evidence.Limit {
	t.Helper()
	for _, limit := range window.Limits {
		if limit.Field == field {
			return limit
		}
	}
	t.Fatalf("field %q missing from %q", field, window.ID)
	return evidence.Limit{}
}

type countingTransport struct{ calls int }

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("unexpected HTTP request")
}
