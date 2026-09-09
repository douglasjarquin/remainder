package cursor_test

import (
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
		AuthFile: "synthetic-auth.json",
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
	exported := make([]string, 0, typeOfOptions.NumField())
	for fieldIndex := range typeOfOptions.NumField() {
		field := typeOfOptions.Field(fieldIndex)
		if field.IsExported() {
			exported = append(exported, field.Name)
		}
	}
	want := []string{"AuthFile", "Endpoint", "Client", "Timeout", "Now"}
	if !reflect.DeepEqual(exported, want) {
		t.Fatalf("exported Options fields = %v, want %v", exported, want)
	}
}
