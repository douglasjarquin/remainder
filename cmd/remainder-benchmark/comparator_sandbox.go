package main

import (
	"bytes"
	"os"
	"path/filepath"
)

type comparatorSandbox struct {
	root string
	env  []string
}

func newComparatorSandbox(preloadPath string) (comparatorSandbox, error) {
	root, err := os.MkdirTemp("", "remainder-quota-axi-")
	if err != nil {
		return comparatorSandbox{}, err
	}
	codexHome := filepath.Join(root, "codex")
	cacheHome := filepath.Join(root, "cache")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		os.RemoveAll(root)
		return comparatorSandbox{}, err
	}
	if err := os.MkdirAll(cacheHome, 0o700); err != nil {
		os.RemoveAll(root)
		return comparatorSandbox{}, err
	}
	auth := []byte(`{"tokens":{"access_token":"synthetic-token","account_id":"acct-1"}}`)
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), auth, 0o600); err != nil {
		os.RemoveAll(root)
		return comparatorSandbox{}, err
	}
	return comparatorSandbox{root: root, env: []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Join(root, "home"),
		"CODEX_HOME=" + codexHome,
		"XDG_CACHE_HOME=" + cacheHome,
		"NODE_OPTIONS=--import=" + preloadPath,
		"QUOTA_AXI_CODEX_BINARY=/nonexistent/remainder-quota-axi-codex",
		"NO_COLOR=1",
		"TERM=dumb",
		"TZ=UTC",
	}}, nil
}

func inspectComparatorCache(root string) comparatorCache {
	const relativePath = "temporary XDG_CACHE_HOME/quota-axi/quotas.json"
	data, err := os.ReadFile(filepath.Join(root, "cache", "quota-axi", "quotas.json"))
	if err != nil {
		return comparatorCache{Status: "missing: " + err.Error(), Path: relativePath}
	}
	if !bytes.Contains(data, []byte(`"status": "fresh"`)) {
		return comparatorCache{Status: "invalid: fresh snapshot absent", Path: relativePath, Bytes: len(data), SHA256: digest(data)}
	}
	return comparatorCache{Status: "fresh-snapshot-written", Path: relativePath, Bytes: len(data), SHA256: digest(data)}
}

func cleanupComparatorSandbox(root string) string {
	if err := os.RemoveAll(root); err != nil {
		return "failed: " + err.Error()
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		return "failed: temporary sandbox still exists"
	}
	return "removed-owned-temporary-sandbox"
}
