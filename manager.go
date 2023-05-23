package shutdown

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// New returns an initialized shutdown manager
func New(options ...Option) *Manager {
	m := &Manager{
		logger:              LogPrinter(log.New(os.Stderr, "[shutdown]: ", log.LstdFlags)),
		StagePS:             Stage{0},
		Stage1:              Stage{1},
		Stage2:              Stage{2},
		Stage3:              Stage{3},
		WarningPrefix:       "WARN: ",
		ErrorPrefix:         "ERROR: ",
		logLockTimeouts:     true,
		StatusTimer:         time.Minute,
		shutdownFinished:    make(chan struct{}),
		currentStage:        Stage{-1},
		shutdownRequestedCh: make(chan struct{}),
		timeouts:            [4]time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second},
	}
	m.shutdownRequested.Store(false)
	for _, option := range options {
		option(m)
	}
	return m
}

// Manager encapsulates all state/settings previously stored at package level
type Manager struct {

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

	// StatusTimer is the time between logging which notifiers are waiting to finish.
	// Should not be changed once shutdown has started.
	StatusTimer time.Duration

	// logLockTimeouts enables log timeout warnings
	// and notifier status updates.
	logLockTimeouts bool

	// logger used for output.
	// This can be exchanged with your own using WithLogPrinter option.
	logger LogPrinter

	sqM              sync.Mutex // Mutex for below
	shutdownQueue    [4][]iNotifier
	shutdownFnQueue  [4][]fnNotify
	shutdownFinished chan struct{} // Closed when shutdown has finished
	currentStage     Stage

	srM                 sync.RWMutex // Mutex for below
	shutdownRequested   atomic.Bool
	shutdownRequestedCh chan struct{}
	timeouts            [4]time.Duration

	onTimeOut func(s Stage, ctx string)
	wg        sync.WaitGroup
}

// SetTimeout sets maximum delay to wait for each stage to finish.
// When the timeout has expired for a stage the next stage will be initiated.
func (m *Manager) SetTimeout(d time.Duration) {
	m.srM.Lock()
	for i := range m.timeouts {
		m.timeouts[i] = d
	}
	m.srM.Unlock()
}

// SetTimeoutN set maximum delay to wait for a specific stage to finish.
// When the timeout expired for a stage the next stage will be initiated.
// The stage can be obtained by using the exported variables called 'Stage1, etc.
func (m *Manager) SetTimeoutN(s Stage, d time.Duration) {
	m.srM.Lock()
	m.timeouts[s.n] = d
	m.srM.Unlock()
}

// PreShutdown will return a Notifier that will be fired as soon as the shutdown.
// is signalled, before locks are released.
// This allows to for instance send signals to upstream servers not to send more requests.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) PreShutdown(ctx ...interface{}) Notifier {
	return m.onShutdown(0, 1, ctx).n
}

// PreShutdownFn registers a function that will be called as soon as the shutdown.
// is signalled, before locks are released.
// This allows to for instance send signals to upstream servers not to send more requests.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) PreShutdownFn(fn func(), ctx ...interface{}) Notifier {
	return m.onFunc(0, 1, fn, ctx)
}

// First returns a notifier that will be called in the first stage of shutdowns.
// If shutdown has started and this stage has already been reached, the notifiers Valid() will be false.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) First(ctx ...interface{}) Notifier {
	return m.onShutdown(1, 1, ctx).n
}

// FirstFn executes a function in the first stage of the shutdown
// If shutdown has started and this stage has already been reached, the notifiers Valid() will be false.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) FirstFn(fn func(), ctx ...interface{}) Notifier {
	return m.onFunc(1, 1, fn, ctx)
}

// Second returns a notifier that will be called in the second stage of shutdowns.
// If shutdown has started and this stage has already been reached, the notifiers Valid() will be false.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) Second(ctx ...interface{}) Notifier {
	return m.onShutdown(2, 1, ctx).n
}

// SecondFn executes a function in the second stage of the shutdown.
// If shutdown has started and this stage has already been reached, the notifiers Valid() will be false.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) SecondFn(fn func(), ctx ...interface{}) Notifier {
	return m.onFunc(2, 1, fn, ctx)
}

// Third returns a notifier that will be called in the third stage of shutdowns.
// If shutdown has started and this stage has already been reached, the notifiers Valid() will be false.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) Third(ctx ...interface{}) Notifier {
	return m.onShutdown(3, 1, ctx).n
}

// ThirdFn executes a function in the third stage of the shutdown.
// If shutdown has started and this stage has already been reached, the notifiers Valid() will be false.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) ThirdFn(fn func(), ctx ...interface{}) Notifier {
	return m.onFunc(3, 1, fn, ctx)
}

// OnSignal will start the shutdown when any of the given signals arrive
//
// A good shutdown default is
//
//	shutdown.OnSignal(0, os.Interrupt, syscall.SIGTERM)
//
// which will do shutdown on Ctrl+C and when the program is terminated.
func (m *Manager) OnSignal(exitCode int, sig ...os.Signal) {
	// capture signal and shut down.
	c := make(chan os.Signal, 1)
	signal.Notify(c, sig...)
	go func() {
		for range c {
			m.Shutdown()
			os.Exit(exitCode)
		}
	}()
}

// Exit performs shutdown operations and exits with the given exit code.
func (m *Manager) Exit(code int) {
	m.Shutdown()
	os.Exit(code)
}

// Shutdown will signal all notifiers in three stages.
// It will first check that all locks have been released - see Lock()
// This method is not safe to call concurrently, as a datarace for shutdownRequested is possible.
// As shutdown is called
func (m *Manager) Shutdown() {
	// if the current value is false, then store true. If we couldn't store true,
	// then shutdown is already initalized
	if !m.shutdownRequested.CompareAndSwap(false, true) {
		// Wait till shutdown finished
		<-m.shutdownFinished
		return
	}

	close(m.shutdownRequestedCh)
	lwg := &m.wg

	// Add a pre-shutdown function that waits for all locks to be released.
	m.PreShutdownFn(func() {
		lwg.Wait()
	})

	m.sqM.Lock()
	for stage := 0; stage < 4; stage++ {
		m.srM.Lock()
		to := m.timeouts[stage]
		m.currentStage = Stage{stage}
		m.srM.Unlock()

		queue := m.shutdownQueue[stage]
		if len(queue) == 0 {
			continue
		}

		if stage == 0 {
			m.logger.Printf("Initiating shutdown %v", time.Now())
		} else {
			m.logger.Printf("Shutdown stage %v", stage)
		}

		wait := make([]chan struct{}, len(queue))
		var calledFrom []string
		if m.logLockTimeouts {
			calledFrom = make([]string, len(queue))
		}
		// Send notification to all waiting
		for i, n := range queue {
			wait[i] = make(chan struct{})
			if m.logLockTimeouts {
				calledFrom[i] = n.calledFrom
			}
			queue[i].n.c <- wait[i]
		}

		// Send notification to all function notifiers, but don't wait
		for _, notifier := range m.shutdownFnQueue[stage] {
			notifier.client.c <- make(chan struct{})
			close(notifier.client.c)
		}

		// We don't lock while we are waiting for notifiers to return
		m.sqM.Unlock()

		// Wait for all to return, no more than the shutdown delay
		timeout := time.After(to)

	brwait:
		for i := range wait {
			var tick <-chan time.Time
			if m.logLockTimeouts {
				tick = time.NewTicker(m.StatusTimer).C
			}
		wloop:
			for {
				select {
				case <-wait[i]:
					break wloop
				case <-timeout:
					if len(calledFrom) > 0 {
						if m.onTimeOut != nil {
							m.onTimeOut(Stage{n: stage}, calledFrom[i])
						}
						m.logger.Printf(m.ErrorPrefix+"Notifier Timed Out: %s", calledFrom[i])
					}
					m.logger.Printf(m.ErrorPrefix+"Timeout waiting to shutdown, forcing shutdown stage %v.", stage)
					break brwait
				case <-tick:
					if len(calledFrom) > 0 {
						m.logger.Printf(m.WarningPrefix+"Stage %d, waiting for notifier (%s)", stage, calledFrom[i])
					}
				}
			}
		}
		m.sqM.Lock()
	}
	// Reset - mainly for tests.
	m.shutdownQueue = [4][]iNotifier{}
	m.shutdownFnQueue = [4][]fnNotify{}
	close(m.shutdownFinished)
	m.sqM.Unlock()
}

// Started returns true if shutdown has been started.
// Note that shutdown can have been started before you check the value.
func (m *Manager) Started() bool {
	return m.shutdownRequested.Load()
}

// StartedCh returns a channel that is closed once shutdown has started.
func (m *Manager) StartedCh() <-chan struct{} {
	return m.shutdownRequestedCh
}

// Wait will wait until shutdown has finished.
// This can be used to keep a main function from exiting
// until shutdown has been called, either by a goroutine
// or a signal.
func (m *Manager) Wait() {
	<-m.shutdownFinished
}

// Lock will signal that you have a function running,
// that you do not want to be interrupted by a shutdown.
//
// The lock is created with a timeout equal to the length of the
// preshutdown stage at the time of creation. When that amount of
// time has expired the lock will be removed, and a warning will
// be printed.
//
// If the function returns nil shutdown has already been initiated,
// and you did not get a lock. You should therefore not call the returned
// function.
//
// If the function did not return nil, you should call the function to unlock
// the lock.
//
// You should not hold a lock when you start a shutdown.
//
// For easier debugging you can send a context that will be printed if the lock
// times out. All supplied context is printed with '%v' formatting.
func (m *Manager) Lock(ctx ...interface{}) func() {
	if m.shutdownRequested.Load() {
		return nil
	}

	m.wg.Add(1)

	var release = make(chan struct{})
	var timeout = time.After(m.timeouts[0])

	// Store what called this
	var calledFrom string
	if m.logLockTimeouts {
		_, file, line, _ := runtime.Caller(1)
		if len(ctx) > 0 {
			calledFrom = fmt.Sprintf("%v. ", ctx)
		}
		calledFrom = fmt.Sprintf("%sCalled from %s:%d", calledFrom, file, line)
	}

	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		select {
		case <-timeout:
			if m.onTimeOut != nil {
				m.onTimeOut(m.StagePS, calledFrom)
			}
			if m.logLockTimeouts {
				m.logger.Printf(m.WarningPrefix+"Lock expired! %s", calledFrom)
			}
		case <-release:
		}
	}(&m.wg)
	return func() { close(release) }
}

// Create a function notifier.
// depth is the call depth of the caller.
func (m *Manager) onFunc(prio, depth int, fn func(), ctx []interface{}) Notifier {
	f := fnNotify{
		internal: m.onShutdown(prio, depth+1, ctx),
		cancel:   make(chan struct{}),
		client:   m.newNotifier(),
	}
	if f.internal.n.c == nil {
		return Notifier{}
	}
	go func() {
		select {
		case <-f.cancel:
			return
		case c := <-f.internal.n.c:
			{
				defer func() {
					if r := recover(); r != nil {
						m.logger.Printf(m.ErrorPrefix+"Panic in shutdown function: %v (%v)", r, f.internal.calledFrom)
						m.logger.Printf("%s", string(debug.Stack()))
					}
					if c != nil {
						close(c)
					}
				}()
				fn()
			}
		}
	}()
	m.sqM.Lock()
	m.shutdownFnQueue[prio] = append(m.shutdownFnQueue[prio], f)
	m.sqM.Unlock()
	return f.client
}

// onShutdown will request a shutdown notifier.
// depth is the call depth of the caller.
func (m *Manager) onShutdown(prio, depth int, ctx []interface{}) iNotifier {
	m.sqM.Lock()
	if m.currentStage.n >= prio {
		m.sqM.Unlock()
		return iNotifier{n: Notifier{}}
	}
	n := m.newNotifier()
	in := iNotifier{n: n}
	if m.logLockTimeouts {
		_, file, line, _ := runtime.Caller(depth + 1)
		in.calledFrom = fmt.Sprintf("%s:%d", file, line)
		if len(ctx) != 0 {
			in.calledFrom = fmt.Sprintf("%v - %s", ctx, in.calledFrom)
		}
	}
	m.shutdownQueue[prio] = append(m.shutdownQueue[prio], in)
	m.sqM.Unlock()
	return in
}

// newNotifier returns a new notifier linked to the manager
func (m *Manager) newNotifier() Notifier {
	return Notifier{c: make(chan chan struct{}, 1), m: m}
}
