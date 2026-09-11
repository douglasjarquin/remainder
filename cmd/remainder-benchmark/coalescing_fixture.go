package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

type coalescingFixture struct {
	caPath   string
	proxyURL string
	mode     atomic.Int32
	delayNS  atomic.Int64
	requests atomic.Int64
	close    func()
}

const (
	fixtureSuccess int32 = iota
	fixtureRateLimited
)

func newCoalescingFixture(root string) (*coalescingFixture, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"chatgpt.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	caPath := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0o600); err != nil {
		return nil, err
	}
	fixture := &coalescingFixture{caPath: caPath}
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(fixture.serveUsage))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{certificate}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	upstream.StartTLS()
	proxy := httptest.NewServer(connectProxy(upstream.Listener.Addr().String()))
	fixture.proxyURL = proxy.URL
	fixture.close = func() {
		proxy.Close()
		upstream.Close()
	}
	return fixture, nil
}

func (f *coalescingFixture) serveUsage(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.URL.Path != "/backend-api/wham/usage" || request.Header.Get("Authorization") != "Bearer synthetic-fixture-token" || request.Header.Get("ChatGPT-Account-Id") != "fixture-account" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.requests.Add(1)
	if delay := time.Duration(f.delayNS.Load()); delay > 0 {
		time.Sleep(delay)
	}
	if f.mode.Load() == fixtureRateLimited {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"account_id":"fixture-account","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000},"secondary_window":{"used_percent":20,"limit_window_seconds":604800}}}`)
}

func connectProxy(upstreamAddress string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect || request.Host != "chatgpt.com:443" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		upstream, err := net.DialTimeout("tcp", upstreamAddress, time.Second)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		defer connection.Close()
		defer upstream.Close()
		_, _ = io.WriteString(connection, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, connection)
			close(done)
		}()
		_, _ = io.Copy(connection, upstream)
		_ = connection.Close()
		<-done
	})
}

func (f *coalescingFixture) environment(home, codexHome, cacheHome, temporary string) []string {
	return []string{
		"HOME=" + home,
		"CODEX_HOME=" + codexHome,
		"XDG_CACHE_HOME=" + cacheHome,
		"TMPDIR=" + temporary,
		"SSL_CERT_FILE=" + f.caPath,
		"SSL_CERT_DIR=" + temporary,
		"HTTPS_PROXY=" + f.proxyURL,
		"NO_PROXY=",
	}
}

func (f *coalescingFixture) String() string {
	return fmt.Sprintf("loopback TLS through allowlisted CONNECT proxy %s", f.proxyURL)
}
