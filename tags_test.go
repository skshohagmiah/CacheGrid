package cachegrid

import (
	"testing"
	"time"
)

func TestSetWithTagsAndInvalidate(t *testing.T) {
	c := newTestCache(t)

	c.SetWithTags("product:1", "laptop", time.Minute, []string{"electronics", "sale"})
	c.SetWithTags("product:2", "phone", time.Minute, []string{"electronics"})
	c.SetWithTags("product:3", "shirt", time.Minute, []string{"clothing", "sale"})

	// All three should exist
	if !c.Exists("product:1") || !c.Exists("product:2") || !c.Exists("product:3") {
		t.Fatal("all products should exist initially")
	}

	// Invalidate "sale" tag
	c.InvalidateTag("sale")

	// product:1 and product:3 should be gone
	if c.Exists("product:1") {
		t.Fatal("product:1 should be invalidated (sale tag)")
	}
	if c.Exists("product:3") {
		t.Fatal("product:3 should be invalidated (sale tag)")
	}

	// product:2 should still exist (no sale tag)
	if !c.Exists("product:2") {
		t.Fatal("product:2 should still exist")
	}
}

func TestInvalidateTagNoKeys(t *testing.T) {
	c := newTestCache(t)
	// Should not panic
	c.InvalidateTag("nonexistent-tag")
}

func TestSetWithTagsOverwrite(t *testing.T) {
	c := newTestCache(t)

	c.SetWithTags("item", "v1", time.Minute, []string{"old-tag"})
	c.SetWithTags("item", "v2", time.Minute, []string{"new-tag"})

	// Invalidating old-tag should NOT affect item (tags were replaced)
	c.InvalidateTag("old-tag")
	if !c.Exists("item") {
		t.Fatal("item should still exist after old-tag invalidation")
	}

	// Invalidating new-tag should remove it
	c.InvalidateTag("new-tag")
	if c.Exists("item") {
		t.Fatal("item should be gone after new-tag invalidation")
	}
}

func TestSetWithTagsEmpty(t *testing.T) {
	c := newTestCache(t)
	// SetWithTags with empty tags should work like regular Set
	err := c.SetWithTags("plain", "value", time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Exists("plain") {
		t.Fatal("plain key should exist")
	}
}
