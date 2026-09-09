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

const WarningStorageUnavailable Warning = "cache storage is unavailable; returned live evidence without caching"

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
	if result, ok := eligible(initial, expectedAccount, policy.MaxAge, s.options.Now()); ok && !policy.Refresh {
		return result, nil
	}
	if policy.Mode == ModeOnly {
		return Result{}, cachedOnlyError(initial)
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
	if result, ok := reuseAfterLock(initial, current, expectedAccount, policy, s.options.Now()); ok {
		return result, nil
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
	if existing.state == recordSupported && existing.record.Observation.ObservedAt.After(observation.ObservedAt) {
		return "", ErrOlderObservation
	}
	random := make([]byte, 16)
	if _, err := io.ReadFull(s.options.Random, random); err != nil {
		return "", fmt.Errorf("create cache generation: %w", err)
	}
	generation := hex.EncodeToString(random)
	value := record{SchemaVersion: recordVersion, BindingHash: bindingHash(binding), Generation: generation, Observation: observation}
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

func eligible(loaded loadedRecord, expectedAccount string, maxAge time.Duration, now time.Time) (Result, bool) {
	if loaded.state != recordSupported || loaded.record.Revoked || (expectedAccount != "" && loaded.record.Observation.Account.LastObserved != expectedAccount) {
		return Result{}, false
	}
	age := now.Sub(loaded.record.Observation.ObservedAt)
	if age < 0 || age > maxAge {
		return Result{}, false
	}
	observation := loaded.record.Observation
	observation.Account.Binding = evidence.IdentityHistorical
	return Result{Observation: observation, Generation: loaded.record.Generation, FromCache: true}, true
}

func reuseAfterLock(initial, current loadedRecord, expectedAccount string, policy Policy, now time.Time) (Result, bool) {
	result, ok := eligible(current, expectedAccount, policy.MaxAge, now)
	if !ok {
		return Result{}, false
	}
	if !policy.Refresh {
		return result, true
	}
	return result, current.record.Generation != initial.record.Generation
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
	if current.state == recordSupported {
		now := s.options.Now()
		wasRevoked := current.record.Revoked
		current.record.LastAttemptAt = &now
		current.record.LastFailure = fetched.Failure
		current.record.Revoked = wasRevoked || fetched.Failure == FailureRevoked || fetched.Failure == FailureAccountMismatch
		if err := writeRecord(s.SnapshotPath(binding), current.record); err != nil {
			return Result{}, fetched.Err
		}
		if policy.StaleOnError && !current.record.Revoked && fetched.Failure == FailureTransient && (expectedAccount == "" || current.record.Observation.Account.LastObserved == expectedAccount) {
			observation := current.record.Observation
			observation.Account.Binding = evidence.IdentityHistorical
			observation.Freshness = evidence.FreshStale
			return Result{Observation: observation, Generation: current.record.Generation, FromCache: true}, nil
		}
	}
	return Result{}, fetched.Err
}
