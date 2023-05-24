package shutdown

import "time"

type Option func(*Manager)

// WithLogPrinter sets the logprinter
func WithLogPrinter(fn func(format string, v ...interface{})) Option {
	return func(m *Manager) {
		m.logger = logWrapper{w: fn}
	}
}

// WithLogLockTimeouts toggles logging timeouts. Default: true
func WithLogLockTimeouts(logTimeouts bool) Option {
	return func(m *Manager) {
		m.logLockTimeouts = logTimeouts
	}
}

// WithOnTimeout allows you to get a notification if a shutdown stage times out.
// The stage and the context of the hanging shutdown/lock function is returned.
func WithOnTimeout(fn func(Stage, string)) Option {
	return func(m *Manager) {
		m.onTimeOut = fn
	}
}

// TODO

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
