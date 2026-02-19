package cache

import "container/list"

// LRUList is a thin wrapper around container/list for LRU ordering.
// The front of the list is the most recently used, the back is the least.
type LRUList struct {
	ll *list.List
}

// NewLRUList creates a new empty LRU list.
func NewLRUList() *LRUList {
	return &LRUList{ll: list.New()}
}

// PushFront inserts an entry at the front (most recently used).
func (l *LRUList) PushFront(e *Entry) *list.Element {
	return l.ll.PushFront(e)
}

// MoveToFront moves the element to the front (most recently used).
func (l *LRUList) MoveToFront(el *list.Element) {
	l.ll.MoveToFront(el)
}

// Remove removes the element from the list.
func (l *LRUList) Remove(el *list.Element) {
	l.ll.Remove(el)
}

// RemoveOldest removes and returns the least recently used entry.
// Returns nil if the list is empty.
func (l *LRUList) RemoveOldest() *Entry {
	el := l.ll.Back()
	if el == nil {
		return nil
	}
	l.ll.Remove(el)
	return el.Value.(*Entry)
}

// Len returns the number of elements in the list.
func (l *LRUList) Len() int {
	return l.ll.Len()
}
