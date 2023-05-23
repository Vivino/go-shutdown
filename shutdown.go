// Copyright (c) 2015 Klaus Post, released under MIT License. See LICENSE file.

// Package shutdown provides management of your shutdown process.
//
// The package will enable you to get notifications for your application and handle the shutdown process.
//
// # See more information about the how to use it in the README.md file
//
// Package home: https://github.com/klauspost/shutdown2
package shutdown

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
)

// Stage contains stage information.
// Valid values for this is exported as variables StageN.
type Stage struct {
	n int
}

// LogPrinter is an interface for writing logging information.
// The writer must handle concurrent writes.
type LogPrinter interface {
	Printf(format string, v ...interface{})
}

// Notifier is a channel, that will be sent a channel
// once the application shuts down.
// When you have performed your shutdown actions close the channel you are given.
type Notifier struct {
	c chan chan struct{}
	m *Manager
}

// Valid returns true if it can be used as a notifier. If false shutdown has already started
func (n Notifier) Valid() bool {
	return n.m != nil
}

// Notify returns a channel to listen to for shutdown events
func (n Notifier) Notify() <-chan chan struct{} {
	return n.c
}

// Internal notifier
type iNotifier struct {
	n          Notifier
	calledFrom string
}
type fnNotify struct {
	client   Notifier
	internal iNotifier
	cancel   chan struct{}
}

type logWrapper struct {
	w func(format string, v ...interface{})
}

func (l logWrapper) Printf(format string, v ...interface{}) {
	l.w(format, v...)
}

// SetLogPrinter will use the specified function to write logging information.
func (m *Manager) SetLogPrinter(fn func(format string, v ...interface{})) {
	m.LoggerMu.Lock()
	m.Logger = logWrapper{w: fn}
	m.LoggerMu.Unlock()
}

// OnTimeout allows you to get a notification if a shutdown stage times out.
// The stage and the context of the hanging shutdown/lock function is returned.
func (m *Manager) OnTimeout(fn func(Stage, string)) {
	m.srM.Lock()
	m.onTimeOut = fn
	m.srM.Unlock()
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

// Cancel a Notifier.
// This will remove a notifier from the shutdown queue,
// and it will not be signalled when shutdown starts.
// If the shutdown has already started this will not have any effect,
// but a goroutine will wait for the notifier to be triggered.
func (s Notifier) Cancel() {
	if s.c == nil {
		return
	}
	s.m.srM.RLock()
	if s.m.shutdownRequested {
		s.m.srM.RUnlock()
		// Wait until we get the notification and close it:
		go func() {
			select {
			case v := <-s.c:
				close(v)
			}
		}()
		return
	}
	s.m.srM.RUnlock()
	s.m.sqM.Lock()
	var a chan chan struct{}
	var b chan chan struct{}
	a = s.c
	for n, sdq := range s.m.shutdownQueue {
		for i, qi := range sdq {
			b = qi.n.c
			if a == b {
				s.m.shutdownQueue[n] = append(s.m.shutdownQueue[n][:i], s.m.shutdownQueue[n][i+1:]...)
			}
		}
		for i, fn := range s.m.shutdownFnQueue[n] {
			b = fn.client.c
			if a == b {
				// Find the matching internal and remove that.
				for i := range s.m.shutdownQueue[n] {
					b = s.m.shutdownQueue[n][i].n.c
					if fn.internal.n.c == b {
						s.m.shutdownQueue[n] = append(s.m.shutdownQueue[n][:i], s.m.shutdownQueue[n][i+1:]...)
						break
					}
				}
				// Cancel, so the goroutine exits.
				close(fn.cancel)
				// Remove this
				s.m.shutdownFnQueue[n] = append(s.m.shutdownFnQueue[n][:i], s.m.shutdownFnQueue[n][i+1:]...)
			}
		}
	}
	s.m.sqM.Unlock()
}

// CancelWait will cancel a Notifier, or wait for it to become active if shutdown has been started.
// This will remove a notifier from the shutdown queue, and it will not be signalled when shutdown starts.
// If the notifier is nil (requested after its stage has started), it will return at once.
// If the shutdown has already started, this will wait for the notifier to be called and close it.
func (s Notifier) CancelWait() {
	if s.c == nil {
		return
	}
	s.m.sqM.Lock()
	var a chan chan struct{}
	var b chan chan struct{}
	a = s.c
	for n, sdq := range s.m.shutdownQueue {
		for i, qi := range sdq {
			b = qi.n.c
			if a == b {
				s.m.shutdownQueue[n] = append(s.m.shutdownQueue[n][:i], s.m.shutdownQueue[n][i+1:]...)
			}
		}
		for i, fn := range s.m.shutdownFnQueue[n] {
			b = fn.client.c
			if a == b {
				// Find the matching internal and remove that.
				for i := range s.m.shutdownQueue[n] {
					b = s.m.shutdownQueue[n][i].n.c
					if fn.internal.n.c == b {
						s.m.shutdownQueue[n] = append(s.m.shutdownQueue[n][:i], s.m.shutdownQueue[n][i+1:]...)
						break
					}
				}
				// Cancel, so the goroutine exits.
				close(fn.cancel)
				// Remove this
				s.m.shutdownFnQueue[n] = append(s.m.shutdownFnQueue[n][:i], s.m.shutdownFnQueue[n][i+1:]...)
			}
		}
	}
	s.m.srM.RLock()
	if s.m.shutdownRequested {
		s.m.sqM.Unlock()
		s.m.srM.RUnlock()
		// Wait until we get the notification and close it:
		select {
		case v := <-s.c:
			close(v)
		}
		return
	}
	s.m.srM.RUnlock()
	s.m.sqM.Unlock()
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
// If shutdown has started and this stage has already been reached, nil will be returned.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) First(ctx ...interface{}) Notifier {
	return m.onShutdown(1, 1, ctx).n
}

// FirstFn executes a function in the first stage of the shutdown
// If shutdown has started and this stage has already been reached, nil will be returned.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) FirstFn(fn func(), ctx ...interface{}) Notifier {
	return m.onFunc(1, 1, fn, ctx)
}

// Second returns a notifier that will be called in the second stage of shutdowns.
// If shutdown has started and this stage has already been reached, nil will be returned.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) Second(ctx ...interface{}) Notifier {
	return m.onShutdown(2, 1, ctx).n
}

// SecondFn executes a function in the second stage of the shutdown.
// If shutdown has started and this stage has already been reached, nil will be returned.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) SecondFn(fn func(), ctx ...interface{}) Notifier {
	return m.onFunc(2, 1, fn, ctx)
}

// Third returns a notifier that will be called in the third stage of shutdowns.
// If shutdown has started and this stage has already been reached, nil will be returned.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) Third(ctx ...interface{}) Notifier {
	return m.onShutdown(3, 1, ctx).n
}

// ThirdFn executes a function in the third stage of the shutdown.
// If shutdown has started and this stage has already been reached, nil will be returned.
// The context is printed if LogLockTimeouts is enabled.
func (m *Manager) ThirdFn(fn func(), ctx ...interface{}) Notifier {
	return m.onFunc(3, 1, fn, ctx)
}

func (m *Manager) newNotifier() Notifier {
	return Notifier{c: make(chan chan struct{}, 1), m: m}
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
						m.LoggerMu.Lock()
						m.Logger.Printf(m.ErrorPrefix+"Panic in shutdown function: %v (%v)", r, f.internal.calledFrom)
						m.Logger.Printf("%s", string(debug.Stack()))
						m.LoggerMu.Unlock()
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
	if m.LogLockTimeouts {
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
func (m *Manager) Shutdown() {
	m.srM.Lock()
	if m.shutdownRequested {
		m.srM.Unlock()
		// Wait till shutdown finished
		<-m.shutdownFinished
		return
	}
	m.shutdownRequested = true
	close(m.shutdownRequestedCh)
	lwg := m.wg
	onTimeOutFn := m.onTimeOut
	m.srM.Unlock()

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
		m.LoggerMu.Lock()
		if stage == 0 {
			m.Logger.Printf("Initiating shutdown %v", time.Now())
		} else {
			m.Logger.Printf("Shutdown stage %v", stage)
		}
		m.LoggerMu.Unlock()
		wait := make([]chan struct{}, len(queue))
		var calledFrom []string
		if m.LogLockTimeouts {
			calledFrom = make([]string, len(queue))
		}
		// Send notification to all waiting
		for i, n := range queue {
			wait[i] = make(chan struct{})
			if m.LogLockTimeouts {
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
			if m.LogLockTimeouts {
				tick = time.Tick(m.StatusTimer)
			}
		wloop:
			for {
				select {
				case <-wait[i]:
					break wloop
				case <-timeout:
					m.LoggerMu.Lock()
					if len(calledFrom) > 0 {
						m.srM.RLock()
						if onTimeOutFn != nil {
							onTimeOutFn(Stage{n: stage}, calledFrom[i])
						}
						m.srM.RUnlock()
						m.Logger.Printf(m.ErrorPrefix+"Notifier Timed Out: %s", calledFrom[i])
					}
					m.Logger.Printf(m.ErrorPrefix+"Timeout waiting to shutdown, forcing shutdown stage %v.", stage)
					m.LoggerMu.Unlock()
					break brwait
				case <-tick:
					if len(calledFrom) > 0 {
						m.LoggerMu.Lock()
						m.Logger.Printf(m.WarningPrefix+"Stage %d, waiting for notifier (%s)", stage, calledFrom[i])
						m.LoggerMu.Unlock()
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
	m.srM.RLock()
	started := m.shutdownRequested
	m.srM.RUnlock()
	return started
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
	m.srM.RLock()
	if m.shutdownRequested {
		m.srM.RUnlock()
		return nil
	}
	m.wg.Add(1)
	onTimeOutFn := m.onTimeOut
	m.srM.RUnlock()
	var release = make(chan struct{}, 0)
	var timeout = time.After(m.timeouts[0])

	// Store what called this
	var calledFrom string
	if m.LogLockTimeouts {
		_, file, line, _ := runtime.Caller(1)
		if len(ctx) > 0 {
			calledFrom = fmt.Sprintf("%v. ", ctx)
		}
		calledFrom = fmt.Sprintf("%sCalled from %s:%d", calledFrom, file, line)
	}

	go func(wg *sync.WaitGroup) {
		select {
		case <-timeout:
			if onTimeOutFn != nil {
				onTimeOutFn(m.StagePS, calledFrom)
			}
			if m.LogLockTimeouts {
				m.LoggerMu.Lock()
				m.Logger.Printf(m.WarningPrefix+"Lock expired! %s", calledFrom)
				m.LoggerMu.Unlock()
			}
		case <-release:
		}
		wg.Done()
	}(&m.wg)
	return func() { close(release) }
}
