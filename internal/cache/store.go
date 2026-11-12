package cache

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

const WarningStorageUnavailable Warning = "cache storage is unavailable; live collection bypasses caching"

type Store struct {
	root    string
	options Options
}

func New(root string, options Options) *Store {
	return &Store{root: filepath.Clean(root), options: options.withDefaults()}
}

func NewUserStore(options Options) (*Store, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user cache directory: %w", err)
	}
	return New(filepath.Join(root, "remainder", "v1"), options), nil
}

func (s *Store) SnapshotPath(binding Binding) string {
	return filepath.Join(s.root, bindingHash(binding), "snapshot.json")
}

func (s *Store) Put(ctx context.Context, binding Binding, observation evidence.Observation) (string, error) {
	if err := binding.Validate(); err != nil {
		return "", err
	}
	directory, err := s.prepare(binding)
	if err != nil {
		return "", err
	}
	lock, err := acquireLock(ctx, filepath.Join(directory, "refresh.lock"), s.options.LockWait)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	return s.putLocked(binding, observation, readRecord(s.SnapshotPath(binding), bindingHash(binding)))
}

func (s *Store) Resolve(ctx context.Context, binding Binding, expectedAccount string, policy Policy, fetch func(context.Context) FetchResult) (Result, error) {
	if err := binding.Validate(); err != nil {
		return Result{}, err
	}
	if err := policy.Validate(); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
	defer cancel()
	if policy.Mode == ModeOff {
		return resolveLive(ctx, expectedAccount, fetch)
	}
	wantBinding := bindingHash(binding)
	initial := readRecord(s.SnapshotPath(binding), wantBinding)
	if result, err, done := served(initial, decide(initial, expectedAccount, policy, s.options.Now(), !policy.Refresh)); done {
		return result, err
	}
	directory, err := s.prepare(binding)
	if err != nil {
		result, fetchErr := resolveLive(ctx, expectedAccount, fetch)
		result.Warning = WarningStorageUnavailable
		result.CacheError = err
		return result, fetchErr
	}
	lock, err := acquireLock(ctx, filepath.Join(directory, "refresh.lock"), s.options.LockWait)
	if err != nil {
		if errors.Is(err, ErrLockTimeout) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{}, err
		}
		result, fetchErr := resolveLive(ctx, expectedAccount, fetch)
		result.Warning = WarningStorageUnavailable
		result.CacheError = err
		return result, fetchErr
	}
	defer lock.Close()
	current := readRecord(s.SnapshotPath(binding), wantBinding)
	refreshedByPeer := current.record.Generation != initial.record.Generation
	hitAllowed := !policy.Refresh || refreshedByPeer
	if result, err, done := served(current, decide(current, expectedAccount, policy, s.options.Now(), hitAllowed)); done {
		return result, err
	}
	fetched := fetch(ctx)
	if fetched.Err != nil {
		return s.handleFailure(binding, current, expectedAccount, policy, fetched)
	}
	if err := validateObservation(binding, expectedAccount, fetched.Observation); err != nil {
		return Result{}, err
	}
	generation, err := s.putLocked(binding, fetched.Observation, current)
	if err != nil {
		return Result{Observation: fetched.Observation, Warning: WarningStorageUnavailable, CacheError: err}, nil
	}
	return Result{Observation: fetched.Observation, Generation: generation}, nil
}

func (s *Store) prepare(binding Binding) (string, error) {
	if s.root == "." || !filepath.IsAbs(s.root) {
		return "", errors.New("cache root must be absolute")
	}
	if err := ensureDirectory(s.root); err != nil {
		return "", err
	}
	directory := filepath.Join(s.root, bindingHash(binding))
	if err := ensureDirectory(directory); err != nil {
		return "", err
	}
	return directory, nil
}

func (s *Store) putLocked(binding Binding, observation evidence.Observation, existing loadedRecord) (string, error) {
	if err := validateObservation(binding, "", observation); err != nil {
		return "", err
	}
	if existing.state == recordUnknown {
		return "", ErrUnknownSchema
	}
	if existing.state == recordSupported && existing.record.Observation != nil && existing.record.Observation.ObservedAt.After(observation.ObservedAt) {
		return "", ErrOlderObservation
	}
	random := make([]byte, 16)
	if _, err := io.ReadFull(s.options.Random, random); err != nil {
		return "", fmt.Errorf("create cache generation: %w", err)
	}
	generation := hex.EncodeToString(random)
	value := record{SchemaVersion: recordVersion, BindingHash: bindingHash(binding), Generation: generation, Observation: &observation}
	if err := writeRecord(s.SnapshotPath(binding), value); err != nil {
		return "", err
	}
	return generation, nil
}

func validateObservation(binding Binding, expectedAccount string, observation evidence.Observation) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	if observation.Provider != binding.Provider || observation.Profile != binding.Profile || observation.Source.Kind != binding.SourceKind || observation.Source.Name != binding.SourceName {
		return ErrInvalidBinding
	}
	if expectedAccount != "" && observation.Account.LastObserved != expectedAccount {
		return evidence.ErrWrongAccount
	}
	return nil
}

func resolveLive(ctx context.Context, expectedAccount string, fetch func(context.Context) FetchResult) (Result, error) {
	result := fetch(ctx)
	if result.Err != nil {
		return Result{}, result.Err
	}
	if expectedAccount != "" && result.Observation.Account.LastObserved != expectedAccount {
		return Result{}, evidence.ErrWrongAccount
	}
	return Result{Observation: result.Observation}, nil
}

type decision uint8

const (
	decisionRefetch decision = iota
	decisionServeCached
	decisionServeStale
	decisionBackedOff
	decisionCacheOnlyMiss
)

func decide(loaded loadedRecord, expectedAccount string, policy Policy, now time.Time, hitAllowed bool) decision {
	if loaded.state != recordSupported {
		if policy.Mode == ModeOnly {
			return decisionCacheOnlyMiss
		}
		return decisionRefetch
	}
	value := loaded.record
	servable := value.Observation != nil && !value.Revoked && (expectedAccount == "" || value.Observation.Account.LastObserved == expectedAccount) && !value.Observation.ObservedAt.After(now)
	if hitAllowed && servable && now.Sub(value.Observation.ObservedAt) <= policy.MaxAge {
		return decisionServeCached
	}
	if policy.Mode == ModeOnly {
		return decisionCacheOnlyMiss
	}
	if value.LastFailure != FailureTransient || value.RetryAt == nil || !now.Before(*value.RetryAt) {
		return decisionRefetch
	}
	if servable && policy.StaleOnError {
		return decisionServeStale
	}
	return decisionBackedOff
}

func served(loaded loadedRecord, outcome decision) (Result, error, bool) {
	switch outcome {
	case decisionServeCached:
		return cachedResult(loaded.record, false), nil, true
	case decisionServeStale:
		return cachedResult(loaded.record, true), nil, true
	case decisionBackedOff:
		return Result{}, &BackoffError{RetryAt: *loaded.record.RetryAt}, true
	case decisionCacheOnlyMiss:
		return Result{}, cachedOnlyError(loaded), true
	default:
		return Result{}, nil, false
	}
}

func cachedResult(value record, stale bool) Result {
	observation := *value.Observation
	if observation.Account.Binding == evidence.IdentityVerified {
		observation.Account.Binding = evidence.IdentityHistorical
	}
	if stale {
		observation.Freshness = evidence.FreshStale
	}
	return Result{Observation: observation, Generation: value.Generation, FromCache: true}
}

func cachedOnlyError(loaded loadedRecord) error {
	switch loaded.state {
	case recordCorrupt:
		return fmt.Errorf("%w: %w", ErrUnavailable, ErrCorrupt)
	case recordUnknown:
		return fmt.Errorf("%w: %w", ErrUnavailable, ErrUnknownSchema)
	default:
		return ErrUnavailable
	}
}

func (s *Store) handleFailure(binding Binding, current loadedRecord, expectedAccount string, policy Policy, fetched FetchResult) (Result, error) {
	if current.state == recordUnknown {
		return Result{}, fetched.Err
	}
	now := s.options.Now()
	failure, providerRetryAt := Classify(fetched.Err)
	value := record{SchemaVersion: recordVersion, BindingHash: bindingHash(binding)}
	if current.state == recordSupported {
		value = current.record
	}
	value.LastAttemptAt = &now
	value.LastFailure = failure
	value.Revoked = value.Revoked || failure == FailureRevoked || failure == FailureAccountMismatch
	value.RetryAt = nil
	if failure == FailureTransient {
		retryAt := s.retryDeadline(now, providerRetryAt)
		value.RetryAt = &retryAt
	}
	if err := writeRecord(s.SnapshotPath(binding), value); err != nil {
		return Result{}, fetched.Err
	}
	if decide(loadedRecord{state: recordSupported, record: value}, expectedAccount, policy, now, false) == decisionServeStale {
		return cachedResult(value, true), nil
	}
	return Result{}, fetched.Err
}
