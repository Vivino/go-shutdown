package shutdown

/*
This file contains various helpful interfaces, primarily used for testing
*/

// Started ...
type Started interface {
	Started() bool
}

// StartedCh ...
type StartedCh interface {
	Started() bool
	StartedCh() <-chan struct{}
}
