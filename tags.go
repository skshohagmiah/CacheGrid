package cachegrid

import (
	"sync"
	"time"
)

// tagIndex maintains the mapping between tags and keys.
type tagIndex struct {
	mu        sync.RWMutex
	tagToKeys map[string]map[string]struct{}
	keyToTags map[string][]string
}

func newTagIndex() *tagIndex {
	return &tagIndex{
		tagToKeys: make(map[string]map[string]struct{}),
		keyToTags: make(map[string][]string),
	}
}

// Add associates a key with the given tags.
func (ti *tagIndex) Add(key string, tags []string) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	// Remove old tags for this key first
	if oldTags, ok := ti.keyToTags[key]; ok {
		for _, tag := range oldTags {
			if keys, ok := ti.tagToKeys[tag]; ok {
				delete(keys, key)
				if len(keys) == 0 {
					delete(ti.tagToKeys, tag)
				}
			}
		}
	}

	ti.keyToTags[key] = tags
	for _, tag := range tags {
		if ti.tagToKeys[tag] == nil {
			ti.tagToKeys[tag] = make(map[string]struct{})
		}
		ti.tagToKeys[tag][key] = struct{}{}
	}
}

// Remove cleans up tag associations when a key is deleted/evicted/expired.
func (ti *tagIndex) Remove(key string) {
	ti.mu.Lock()
	defer ti.mu.Unlock()

	tags, ok := ti.keyToTags[key]
	if !ok {
		return
	}
	for _, tag := range tags {
		if keys, ok := ti.tagToKeys[tag]; ok {
			delete(keys, key)
			if len(keys) == 0 {
				delete(ti.tagToKeys, tag)
			}
		}
	}
	delete(ti.keyToTags, key)
}

// GetKeysByTag returns all keys associated with a tag.
func (ti *tagIndex) GetKeysByTag(tag string) []string {
	ti.mu.RLock()
	defer ti.mu.RUnlock()

	keys, ok := ti.tagToKeys[tag]
	if !ok {
		return nil
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	return result
}

// SetWithTags stores a value with associated tags.
func (c *Cache) SetWithTags(key string, value interface{}, ttl time.Duration, tags []string) error {
	if err := c.Set(key, value, ttl); err != nil {
		return err
	}
	if len(tags) > 0 {
		c.tags.Add(key, tags)
	}
	return nil
}

// InvalidateTag deletes all keys associated with the given tag.
func (c *Cache) InvalidateTag(tag string) {
	keys := c.tags.GetKeysByTag(tag)
	for _, key := range keys {
		c.Delete(key)
	}
}
