package cursor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/douglasjarquin/remainder/internal/cache"
)

func (a Adapter) postRPC(ctx context.Context, token, method string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint+rpcPath(method), bytes.NewBufferString("{}"))
	if err != nil {
		return nil, fmt.Errorf("%w: Cursor quota endpoint is invalid", ErrInvalidResponse)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	response, err := a.client.Do(req)
	if err != nil {
		return nil, cache.RequestFailure(ctx, "Cursor")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return nil, &cache.FetchError{Kind: cache.FailureRevoked, Err: errors.New("Cursor CLI authentication was rejected")}
	case http.StatusForbidden:
		return nil, &cache.FetchError{Kind: cache.FailurePermanent, Err: errForbidden}
	case http.StatusTooManyRequests:
		return nil, &cache.FetchError{Kind: cache.FailureTransient, RetryAt: cache.ParseRetryAfter(response.Header.Get("Retry-After"), a.now()), Err: errors.New("Cursor quota endpoint is rate limited")}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &cache.FetchError{Kind: cache.FailureTransient, Err: fmt.Errorf("Cursor quota endpoint returned HTTP %d", response.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &cache.FetchError{Kind: cache.FailureTransient, Err: errors.New("Cursor quota response could not be read")}
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("%w: Cursor quota response is too large", ErrInvalidResponse)
	}
	return body, nil
}
