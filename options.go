package shutdown

type Option func(*Manager)

// WithLogPrinter sets the logprinter
func WithLogPrinter(fn func(format string, v ...interface{})) Option {
	return func(m *Manager) {
		m.logger = logWrapper{w: fn}
	}
}
