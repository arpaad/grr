package grr

// IsRegistered reports whether key is registered in this registry or any
// of its parents.
func (r *Registry) IsRegistered(key string) bool {
	return r.findEntry(key) != nil
}

// Keys returns the keys registered directly in this registry, not walking
// the parent chain. Order is unspecified. Mainly for introspection and
// startup validation (see gold.Validate).
func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	return keys
}

// Clear removes all entries from this registry. It does not touch the
// parent chain or any in-flight scopes — mainly useful for test teardown.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[string]*entry)
}
