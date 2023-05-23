package shutdown

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
