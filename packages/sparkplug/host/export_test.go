package host

// AttachForTest binds a publisher as a connected broker session would.
func AttachForTest(e *Engine, p Publisher, ts uint64) { e.attach(p, ts) }
