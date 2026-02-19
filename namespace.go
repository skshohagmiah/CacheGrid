package cachegrid

import "time"

// NamespacedCache wraps a Cache and prefixes all keys with a namespace.
type NamespacedCache struct {
	cache     *Cache
	namespace string
}

// WithNamespace returns a namespaced view of the cache.
// All keys will be prefixed with "namespace:".
func (c *Cache) WithNamespace(namespace string) *NamespacedCache {
	return &NamespacedCache{cache: c, namespace: namespace}
}

func (nc *NamespacedCache) prefixKey(key string) string {
	return nc.namespace + ":" + key
}

func (nc *NamespacedCache) prefixTag(tag string) string {
	return nc.namespace + ":" + tag
}

// Set stores a value in the namespaced cache.
func (nc *NamespacedCache) Set(key string, value interface{}, ttl time.Duration) error {
	return nc.cache.Set(nc.prefixKey(key), value, ttl)
}

// Get retrieves a value from the namespaced cache.
func (nc *NamespacedCache) Get(key string, dest interface{}) bool {
	return nc.cache.Get(nc.prefixKey(key), dest)
}

// Delete removes a key from the namespaced cache.
func (nc *NamespacedCache) Delete(key string) {
	nc.cache.Delete(nc.prefixKey(key))
}

// Exists checks if a key exists in the namespaced cache.
func (nc *NamespacedCache) Exists(key string) bool {
	return nc.cache.Exists(nc.prefixKey(key))
}

// TTL returns the TTL for a key in the namespaced cache.
func (nc *NamespacedCache) TTL(key string) time.Duration {
	return nc.cache.TTL(nc.prefixKey(key))
}

// GetOrSet retrieves or computes a value in the namespaced cache.
func (nc *NamespacedCache) GetOrSet(key string, dest interface{}, ttl time.Duration, fn func() (interface{}, error)) error {
	return nc.cache.GetOrSet(nc.prefixKey(key), dest, ttl, fn)
}

// Incr increments a counter in the namespaced cache.
func (nc *NamespacedCache) Incr(key string, delta int64) (int64, error) {
	return nc.cache.Incr(nc.prefixKey(key), delta)
}

// Decr decrements a counter in the namespaced cache.
func (nc *NamespacedCache) Decr(key string, delta int64) (int64, error) {
	return nc.cache.Decr(nc.prefixKey(key), delta)
}

// SetWithTags stores a value with tags in the namespaced cache.
func (nc *NamespacedCache) SetWithTags(key string, value interface{}, ttl time.Duration, tags []string) error {
	prefixedTags := make([]string, len(tags))
	for i, tag := range tags {
		prefixedTags[i] = nc.prefixTag(tag)
	}
	return nc.cache.SetWithTags(nc.prefixKey(key), value, ttl, prefixedTags)
}

// InvalidateTag invalidates all keys with the given tag in the namespace.
func (nc *NamespacedCache) InvalidateTag(tag string) {
	nc.cache.InvalidateTag(nc.prefixTag(tag))
}
