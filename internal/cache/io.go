package cache

import (
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const maxRecordBytes = 4 << 20

var (
	durationMarshalers = json.MarshalFunc(func(duration time.Duration) ([]byte, error) {
		return []byte(strconv.FormatInt(int64(duration), 10)), nil
	})
	durationUnmarshalers = json.UnmarshalFunc(func(data []byte, duration *time.Duration) error {
		value, err := strconv.ParseInt(string(data), 10, 64)
		if err != nil {
			return err
		}
		*duration = time.Duration(value)
		return nil
	})
)

func bindingHash(binding Binding) string {
	value := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s", binding.Provider, binding.Profile, binding.ResponseBoundary, binding.SourceKind, binding.SourceName, binding.CredentialFingerprint)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func ensureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("cache path is not a directory")
	}
	return os.Chmod(path, 0o700)
}

func readRecord(path, wantBinding string) loadedRecord {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return loadedRecord{state: recordMissing}
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxRecordBytes || info.Mode().Perm()&0o077 != 0 {
		return loadedRecord{state: recordUnavailable, err: err}
	}
	file, err := os.Open(path)
	if err != nil {
		return loadedRecord{state: recordUnavailable, err: err}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRecordBytes+1))
	if err != nil || len(data) > maxRecordBytes {
		return loadedRecord{state: recordCorrupt, err: err}
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return loadedRecord{state: recordCorrupt, err: err}
	}
	if envelope.SchemaVersion != recordVersion {
		return loadedRecord{state: recordUnknown}
	}
	var value record
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true), json.WithUnmarshalers(durationUnmarshalers)); err != nil {
		return loadedRecord{state: recordCorrupt, err: err}
	}
	generation, generationErr := hex.DecodeString(value.Generation)
	hasObservation := value.Observation != nil
	validGeneration := hasObservation && generationErr == nil && len(generation) == 16 || !hasObservation && value.Generation == ""
	validObservation := !hasObservation || value.Observation.Validate() == nil
	validFailure := value.LastFailure == FailureNone || value.LastFailure == FailureTransient || value.LastFailure == FailurePermanent || value.LastFailure == FailureRevoked || value.LastFailure == FailureAccountMismatch
	validRevocation := !value.Revoked || value.LastFailure != FailureNone
	validAttempt := value.LastFailure == FailureNone && value.LastAttemptAt == nil && value.RetryAt == nil && !value.Revoked || value.LastFailure != FailureNone && value.LastAttemptAt != nil
	validRetry := value.RetryAt == nil || value.LastFailure == FailureTransient
	if value.BindingHash != wantBinding || !validGeneration || !validObservation || !validFailure || !validRevocation || !validAttempt || !validRetry {
		return loadedRecord{state: recordCorrupt}
	}
	return loadedRecord{state: recordSupported, record: value}
}

func writeRecord(path string, value record) error {
	directory := filepath.Dir(path)
	data, err := json.Marshal(value, json.Deterministic(true), json.WithMarshalers(durationMarshalers))
	if err != nil {
		return fmt.Errorf("encode cache record: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".snapshot-")
	if err != nil {
		return fmt.Errorf("create cache snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure cache snapshot: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write cache snapshot: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync cache snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close cache snapshot: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace cache snapshot: %w", err)
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open cache directory: %w", err)
	}
	defer directoryFile.Close()
	if err := directoryFile.Sync(); err != nil {
		return fmt.Errorf("sync cache directory: %w", err)
	}
	return nil
}
