package shutdown

// NewManager returns an initialized manager
func NewManager() *Manager {
	m := &Manager{}
	return m
}

// Manager encapsulates all state/settings previously stored at package level
type Manager struct {
}
