package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

type controlledRequestValidator struct {
	provider            string
	requests            atomic.Int64
	profileRequests     atomic.Int64
	usageRequests       atomic.Int64
	cursorUsageRequests atomic.Int64
	cursorPlanRequests  atomic.Int64
	cursorSandRequests  atomic.Int64
	mu                  sync.Mutex
	failures            []string
}

func (v *controlledRequestValidator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v.requests.Add(1)
	if v.provider == "claude" {
		v.serveClaude(w, r)
		return
	}
	if v.provider == "grok" {
		v.serveGrok(w, r)
		return
	}
	if v.provider == "cursor" {
		v.serveCursor(w, r)
		return
	}
	v.serveCodex(w, r)
}

func (v *controlledRequestValidator) serveGrok(w http.ResponseWriter, r *http.Request) {
	v.validateAndWrite(w, r, []struct {
		valid   bool
		failure string
	}{
		{r.Method == http.MethodPost, "method mismatch"},
		{r.URL.Path == "/", "path mismatch"},
		{r.Header.Get("Authorization") == "Bearer synthetic-secret", "authorization header mismatch"},
		{r.Header.Get("Accept") == "*/*", "accept header mismatch"},
		{r.Header.Get("Content-Type") == "application/grpc-web+proto", "content type mismatch"},
		{r.Header.Get("Origin") == "https://grok.com", "origin header mismatch"},
		{r.Header.Get("Referer") == "https://grok.com/?_s=usage", "referer header mismatch"},
		{r.Header.Get("X-Grpc-Web") == "1", "gRPC web header mismatch"},
		{r.Header.Get("X-User-Agent") == "connect-es/2.1.1", "user agent header mismatch"},
	}, string(grokControlledResponse()))
}

func grokControlledResponse() []byte {
	return []byte{0, 0, 0, 0, 7, 10, 5, 13, 0, 0, 32, 66, 128, 0, 0, 0, 16, 'g', 'r', 'p', 'c', '-', 's', 't', 'a', 't', 'u', 's', ':', ' ', '0', '\r', '\n'}
}

func (v *controlledRequestValidator) serveCursor(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 3))
	path := r.URL.Path
	switch path {
	case "/aiserver.v1.DashboardService/GetCurrentPeriodUsage":
		v.cursorUsageRequests.Add(1)
	case "/aiserver.v1.DashboardService/GetPlanInfo":
		v.cursorPlanRequests.Add(1)
	case "/aiserver.v1.DashboardService/GetSandUsageStatus":
		v.cursorSandRequests.Add(1)
	}
	response := "{}"
	if path == "/aiserver.v1.DashboardService/GetCurrentPeriodUsage" {
		response = `{"planUsage":{"totalPercentUsed":40}}`
	} else if path == "/aiserver.v1.DashboardService/GetPlanInfo" {
		response = `{"planInfo":{"planName":"Pro"}}`
	}
	v.validateAndWrite(w, r, []struct {
		valid   bool
		failure string
	}{
		{err == nil, "request body read failed"},
		{r.Method == http.MethodPost, "method mismatch"},
		{path == "/aiserver.v1.DashboardService/GetCurrentPeriodUsage" || path == "/aiserver.v1.DashboardService/GetPlanInfo" || path == "/aiserver.v1.DashboardService/GetSandUsageStatus", "path mismatch"},
		{string(body) == "{}", "body mismatch"},
		{r.Header.Get("Authorization") == "Bearer synthetic-secret", "authorization header mismatch"},
		{r.Header.Get("Accept") == "application/json", "accept header mismatch"},
		{r.Header.Get("Content-Type") == "application/json", "content type header mismatch"},
		{r.Header.Get("Connect-Protocol-Version") == "1", "connect protocol header mismatch"},
	}, response)
}

func (v *controlledRequestValidator) serveCodex(w http.ResponseWriter, r *http.Request) {
	v.validateAndWrite(w, r, []struct {
		valid   bool
		failure string
	}{
		{r.Method == http.MethodGet, "method mismatch"},
		{r.URL.Path == "/backend-api/wham/usage", "path mismatch"},
		{r.Header.Get("Authorization") == "Bearer synthetic-secret", "authorization header mismatch"},
		{r.Header.Get("ChatGPT-Account-Id") == "acct-test", "account header mismatch"},
		{r.Header.Get("Accept") == "application/json", "accept header mismatch"},
	}, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
}

func (v *controlledRequestValidator) serveClaude(w http.ResponseWriter, r *http.Request) {
	if !v.validate(w, []struct {
		valid   bool
		failure string
	}{
		{r.Method == http.MethodGet, "method mismatch"},
		{r.URL.Path == "/profile" || r.URL.Path == "/usage", "path mismatch"},
		{r.Header.Get("Authorization") == "Bearer synthetic-secret", "authorization header mismatch"},
		{r.Header.Get("anthropic-beta") == "oauth-2025-04-20", "anthropic beta header mismatch"},
		{r.Header.Get("Accept") == "application/json", "accept header mismatch"},
		{r.Header.Get("ChatGPT-Account-Id") == "", "unexpected account header"},
	}) {
		return
	}
	switch r.URL.Path {
	case "/profile":
		v.profileRequests.Add(1)
		fmt.Fprint(w, `{"account":{"uuid":"acct-test"}}`)
	case "/usage":
		v.usageRequests.Add(1)
		fmt.Fprint(w, `{"limits":[{"group":"session","percent":40}]}`)
	}
}

func (v *controlledRequestValidator) validateAndWrite(w http.ResponseWriter, r *http.Request, checks []struct {
	valid   bool
	failure string
}, body string,
) {
	if !v.validate(w, checks) {
		return
	}
	fmt.Fprint(w, body)
}

func (v *controlledRequestValidator) validate(w http.ResponseWriter, checks []struct {
	valid   bool
	failure string
},
) bool {
	for _, check := range checks {
		if !check.valid {
			v.mu.Lock()
			v.failures = append(v.failures, check.failure)
			v.mu.Unlock()
		}
	}
	v.mu.Lock()
	failed := len(v.failures) > 0
	v.mu.Unlock()
	if failed {
		http.Error(w, "invalid controlled request", http.StatusBadRequest)
	}
	return !failed
}

func (v *controlledRequestValidator) failure() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.failures) == 0 {
		return nil
	}
	return fmt.Errorf("controlled request: %s", strings.Join(v.failures, ", "))
}

func (v *controlledRequestValidator) countFailure(provider string, beforeRequests, beforeProfile, beforeUsage, beforeCursorUsage, beforeCursorPlan, beforeCursorSand int64) error {
	requests := v.requests.Load() - beforeRequests
	profile := v.profileRequests.Load() - beforeProfile
	usage := v.usageRequests.Load() - beforeUsage
	cursorUsage := v.cursorUsageRequests.Load() - beforeCursorUsage
	cursorPlan := v.cursorPlanRequests.Load() - beforeCursorPlan
	cursorSand := v.cursorSandRequests.Load() - beforeCursorSand
	switch provider {
	case "codex":
		if requests == 1 && profile == 0 && usage == 0 {
			return nil
		}
	case "claude":
		if requests == 2 && profile == 1 && usage == 1 {
			return nil
		}
	case "grok":
		if requests == 1 && profile == 0 && usage == 0 {
			return nil
		}
	case "cursor":
		if requests == 3 && profile == 0 && usage == 0 && cursorUsage == 1 && cursorPlan == 1 && cursorSand == 1 {
			return nil
		}
	}
	return fmt.Errorf("request counts: provider=%s total=%d profile=%d usage=%d cursor_usage=%d cursor_plan=%d cursor_sand=%d", provider, requests, profile, usage, cursorUsage, cursorPlan, cursorSand)
}
