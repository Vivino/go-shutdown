package shutdown

import "time"

type Option func(*Manager)

// WithOSExit toggles calling os.Exit.
func WithOSExit(b bool) Option {
	return func(m *Manager) {
		m.performOSExit = b
	}
}

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

// WithTimeout sets maximum delay to wait for each stage to finish.
// When the timeout has expired for a stage the next stage will be initiated.
func WithTimeout(d time.Duration) Option {
	return func(m *Manager) {
		for i := range m.timeouts {
			m.timeouts[i] = d
		}
	}
}

// WithTimeoutN set maximum delay to wait for a specific stage to finish.
// When the timeout expired for a stage the next stage will be initiated.
// The stage can be obtained by using the exported variables called 'Stage1, etc.
func WithTimeoutN(s Stage, d time.Duration) Option {
	return func(m *Manager) {
		m.timeouts[s.n] = d
	}
}

// WithWarningPrefix is printed before warnings.
func WithWarningPrefix(s string) Option {
	return func(m *Manager) {
		m.warningPrefix = s
	}
}

// WithErrorPrefix is printed before errors.
func WithnErrorPrefix(s string) Option {
	return func(m *Manager) {
		m.warningPrefix = s
	}
}

// WithStatusTimer is the time between logging which notifiers are waiting to finish.
func WithStatusTimer(statusTimer time.Duration) Option {
	return func(m *Manager) {
		m.statusTimer = statusTimer
	}
}
