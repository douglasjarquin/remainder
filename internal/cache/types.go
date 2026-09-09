package cache

import (
	"crypto/rand"
	"errors"
	"io"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

const recordVersion = "v1"

var (
	ErrUnavailable      = errors.New("cached provider evidence is unavailable")
	ErrInvalidPolicy    = errors.New("invalid cache policy")
	ErrInvalidBinding   = errors.New("invalid cache binding")
	ErrCorrupt          = errors.New("cache record is corrupt")
	ErrUnknownSchema    = errors.New("cache record schema is unsupported")
	ErrLockTimeout      = errors.New("cache refresh ownership wait timed out")
	ErrOlderObservation = errors.New("older observation cannot replace newer cache data")
)

type Mode string

const (
	ModeAuto Mode = "auto"
	ModeOff  Mode = "off"
	ModeOnly Mode = "only"
)

type Policy struct {
	Mode         Mode
	MaxAge       time.Duration
	Refresh      bool
	StaleOnError bool
}

func (p Policy) Validate() error {
	if p.MaxAge < 0 {
		return ErrInvalidPolicy
	}
	switch p.Mode {
	case ModeAuto:
		return nil
	case ModeOnly:
		if p.Refresh || p.StaleOnError {
			return ErrInvalidPolicy
		}
		return nil
	case ModeOff:
		if p.Refresh || p.StaleOnError || p.MaxAge != 0 {
			return ErrInvalidPolicy
		}
		return nil
	default:
		return ErrInvalidPolicy
	}
}

type Binding struct {
	Provider              evidence.Provider
	Profile               evidence.Profile
	ResponseBoundary      string
	SourceKind            string
	SourceName            string
	CredentialFingerprint string
}

func (b Binding) Validate() error {
	if b.Provider == "" || b.Profile == "" || b.ResponseBoundary == "" || b.SourceKind == "" || b.SourceName == "" || b.CredentialFingerprint == "" {
		return ErrInvalidBinding
	}
	return nil
}

type FailureKind string

const (
	FailureNone            FailureKind = ""
	FailureTransient       FailureKind = "transient"
	FailurePermanent       FailureKind = "permanent"
	FailureRevoked         FailureKind = "revoked"
	FailureAccountMismatch FailureKind = "account_mismatch"
)

type FetchResult struct {
	Observation evidence.Observation
	Failure     FailureKind
	Err         error
}

type Result struct {
	Observation evidence.Observation
	Generation  string
	FromCache   bool
	Warning     Warning
	CacheError  error
}

type Warning string

type Options struct {
	Now              func() time.Time
	Random           io.Reader
	LockWait         time.Duration
	OperationTimeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.Now == nil {
		o.Now = func() time.Time { return time.Now().UTC() }
	}
	if o.Random == nil {
		o.Random = rand.Reader
	}
	if o.LockWait <= 0 {
		o.LockWait = 2 * time.Second
	}
	if o.OperationTimeout <= 0 {
		o.OperationTimeout = 15 * time.Second
	}
	return o
}
