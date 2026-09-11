package cursor

import (
	"errors"
	"net/http"
	"testing"

	"github.com/douglasjarquin/remainder/internal/cache"
)

func TestSupplementalUnauthorizedRevokesSource(t *testing.T) {
	for _, method := range []string{"GetPlanInfo", "GetSandUsageStatus"} {
		t.Run(method, func(t *testing.T) {
			responses := map[string]fixtureResponse{
				"GetCurrentPeriodUsage": {body: `{"planUsage":{"totalPercentUsed":25}}`},
				"GetPlanInfo":           {body: `{}`},
				"GetSandUsageStatus":    {body: `{}`},
			}
			responses[method] = fixtureResponse{status: http.StatusUnauthorized}
			server := fixtureServer(t, responses)
			defer server.Close()
			adapter := testAdapter(t, server)
			observation, err := adapter.Observe(t.Context(), cursorRequest())
			kind, _ := adapter.Failure(err)
			if !errors.Is(err, ErrRevoked) || len(observation.Windows) != 0 || kind != cache.FailureRevoked {
				t.Fatalf("401 at %s returned outcome=%q windows=%d err=%v kind=%q, want revoked source with no quota", method, observation.Outcome, len(observation.Windows), err, kind)
			}
		})
	}
}
