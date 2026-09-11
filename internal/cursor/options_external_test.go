package cursor_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/cursor"
)

func TestOptions_exposesOnlyFixtureDependencies(t *testing.T) {
	// Given
	options := cursor.Options{
		AuthFile:   "synthetic-auth.json",
		ConfigFile: "synthetic-config.json",
		KeychainReader: func(context.Context) (string, error) {
			return "synthetic-token", nil
		},
		Endpoint: "http://127.0.0.1:1",
		Client:   &http.Client{},
		Timeout:  time.Second,
		Now:      func() time.Time { return time.Unix(1, 0) },
	}

	// When
	adapter := cursor.New(options)
	kind, retryAt := adapter.Failure(nil)

	// Then
	if kind != cache.FailureNone || !retryAt.IsZero() {
		t.Fatalf("Adapter.Failure(nil) = %s/%s", kind, retryAt)
	}
	typeOfOptions := reflect.TypeFor[cursor.Options]()
	if _, exposed := typeOfOptions.FieldByName("GOOS"); exposed {
		t.Fatal("Options exposes an operating-system override")
	}
}
