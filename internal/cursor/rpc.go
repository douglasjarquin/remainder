package cursor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: Cursor quota request canceled or timed out: %w", ErrTransient, ctx.Err())
		}
		return nil, fmt.Errorf("%w: Cursor quota request failed", ErrTransient)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return nil, ErrRevoked
	case http.StatusForbidden:
		return nil, ErrForbidden
	case http.StatusTooManyRequests:
		return nil, &RetryError{RetryAt: parseRetryAfter(response.Header.Get("Retry-After"), a.now())}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: Cursor quota endpoint returned HTTP %d", ErrTransient, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: Cursor quota response could not be read", ErrTransient)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("%w: Cursor quota response is too large", ErrInvalidResponse)
	}
	return body, nil
}
