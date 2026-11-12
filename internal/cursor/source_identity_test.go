package cursor

import (
	"context"
	"testing"
	"time"
)

func TestSourceIdentity_cacheBindingMatchesObservation(t *testing.T) {
	responses := map[string]fixtureResponse{
		"GetCurrentPeriodUsage": {bodyFile: "usage-zero.json"},
		"GetPlanInfo":           {body: `{}`},
		"GetSandUsageStatus":    {body: `{}`},
	}
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			server := fixtureServer(t, responses)
			defer server.Close()
			adapter := New(Options{
				AuthFile:   writeAuth(t, "synthetic-secret"),
				ConfigFile: writeMacConfig(t, `{"authInfo":{"email":"person@example.test","userId":"user-1"}}`),
				KeychainReader: func(context.Context) (string, error) {
					return "synthetic-secret\n", nil
				},
				Endpoint: server.URL,
				Client:   server.Client(),
				Now:      func() time.Time { return fixtureNow },
				Timeout:  time.Second,
				goos:     goos,
			}).WithKeychainPrompt()

			binding, bindingErr := adapter.CacheBinding(t.Context(), cursorRequest())
			observation, observeErr := adapter.Observe(t.Context(), cursorRequest())

			if bindingErr != nil || observeErr != nil || binding.SourceKind != observation.Source.Kind || binding.SourceName != observation.Source.Name {
				t.Fatalf("binding = %+v (%v), source = %+v (%v)", binding, bindingErr, observation.Source, observeErr)
			}
		})
	}
}
