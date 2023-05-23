package shutdown

import (
	"log"
	"os"
	"sync"
	"time"
)

// NewManager returns an initialized manager
func NewManager() *Manager {
	m := &Manager{
		Logger:              LogPrinter(log.New(os.Stderr, "[shutdown]: ", log.LstdFlags)),
		StagePS:             Stage{0},
		Stage1:              Stage{1},
		Stage2:              Stage{2},
		Stage3:              Stage{3},
		WarningPrefix:       "WARN: ",
		ErrorPrefix:         "ERROR: ",
		LogLockTimeouts:     true,
		StatusTimer:         time.Minute,
		shutdownFinished:    make(chan struct{}, 0),
		currentStage:        Stage{-1},
		shutdownRequested:   false,
		shutdownRequestedCh: make(chan struct{}),
		timeouts:            [4]time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second},
	}
	return m
}

// Manager encapsulates all state/settings previously stored at package level
type Manager struct {
	// Logger used for output.
	// This can be exchanged with your own.
	Logger LogPrinter

	// LoggerMu is a mutex for the Logger
	LoggerMu sync.Mutex

	// StagePS indicates the pre shutdown stage when waiting for locks to be released.
	StagePS Stage

	// Stage1 Indicates first stage of timeouts.
	Stage1 Stage

	// Stage2 Indicates second stage of timeouts.
	Stage2 Stage

	// Stage3 indicates third stage of timeouts.
	Stage3 Stage

	// WarningPrefix is printed before warnings.
	WarningPrefix string

	// ErrorPrefix is printed before errors.
	ErrorPrefix string

	// LogLockTimeouts enables log timeout warnings
	// and notifier status updates.
	// Should not be changed once shutdown has started.
	LogLockTimeouts bool

	// StatusTimer is the time between logging which notifiers are waiting to finish.
	// Should not be changed once shutdown has started.
	StatusTimer time.Duration

	sqM              sync.Mutex // Mutex for below
	shutdownQueue    [4][]iNotifier
	shutdownFnQueue  [4][]fnNotify
	shutdownFinished chan struct{} // Closed when shutdown has finished
	currentStage     Stage

	srM                 sync.RWMutex // Mutex for below
	shutdownRequested   bool
	shutdownRequestedCh chan struct{}
	timeouts            [4]time.Duration

	onTimeOut func(s Stage, ctx string)
	wg        sync.WaitGroup
}
