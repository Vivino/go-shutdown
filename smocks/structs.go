package smocks

// NeverStarts Started method always returns false, and channels are never closed
type NeverStarts struct{}

// Started returns false
func (NeverStarts) Started() bool { return false }

// StartedCh returns a níl channel
func (NeverStarts) StartedCh() <-chan struct{} { return nil }
