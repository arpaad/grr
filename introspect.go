package grr

// IsRegistered reports whether key is registered in this registry or any
// of its parents.
func (r *Registry) IsRegistered(key string) bool {
	return r.findEntry(key) != nil
}

// Clear removes all entries from this registry. It does not touch the
// parent chain or any in-flight scopes — mainly useful for test teardown.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[string]*entry)
}
