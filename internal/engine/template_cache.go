package engine

import (
	"container/list"
	"sync"
	"sync/atomic"
	"text/template"
	"text/template/parse"
)

// defaultTemplateCacheCapacity is the default maximum number of compiled ASTs retained in the LRU cache.
const defaultTemplateCacheCapacity = 1024

// cachedTemplate holds the precompiled template AST and whether it invokes any context-dependent functions.
type cachedTemplate struct {
	tmpl    *template.Template
	dynamic bool
}

type templateLRUEntry struct {
	hash [32]byte
	item *cachedTemplate
}

// templateLRUCache is a thread-safe LRU cache for precompiled Go template ASTs.
type templateLRUCache struct {
	mu        sync.Mutex
	capacity  int
	items     map[[32]byte]*list.Element
	evictList *list.List
	hits      atomic.Uint64
	misses    atomic.Uint64
}

func newTemplateLRUCache(capacity int) *templateLRUCache {
	if capacity <= 0 {
		capacity = defaultTemplateCacheCapacity
	}
	return &templateLRUCache{
		capacity:  capacity,
		items:     make(map[[32]byte]*list.Element),
		evictList: list.New(),
	}
}

// Get looks up a precompiled template AST by its SHA-256 hash.
// If found, it promotes the entry to the front of the LRU eviction list and returns (item, true).
func (c *templateLRUCache) Get(h [32]byte) (*cachedTemplate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[h]; ok {
		c.evictList.MoveToFront(elem)
		c.hits.Add(1)
		return elem.Value.(*templateLRUEntry).item, true
	}
	c.misses.Add(1)
	return nil, false
}

// Put inserts a precompiled template AST into the LRU cache.
// If the entry already exists, it is promoted to the front and its template updated.
// If the cache is full, the least recently used entry is evicted.
func (c *templateLRUCache) Put(h [32]byte, item *cachedTemplate) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[h]; ok {
		c.evictList.MoveToFront(elem)
		elem.Value.(*templateLRUEntry).item = item
		return
	}

	if c.evictList.Len() >= c.capacity {
		oldest := c.evictList.Back()
		if oldest != nil {
			c.evictList.Remove(oldest)
			entry := oldest.Value.(*templateLRUEntry)
			delete(c.items, entry.hash)
		}
	}

	elem := c.evictList.PushFront(&templateLRUEntry{
		hash: h,
		item: item,
	})
	c.items[h] = elem
}

// Len returns the current number of cached compiled templates.
func (c *templateLRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evictList.Len()
}

// Stats returns the cumulative cache hits and misses.
func (c *templateLRUCache) Stats() (hits, misses uint64) {
	return c.hits.Load(), c.misses.Load()
}

// Purge removes all cached templates and resets hit/miss counters.
func (c *templateLRUCache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[[32]byte]*list.Element)
	c.evictList.Init()
	c.hits.Store(0)
	c.misses.Store(0)
}

// templateUsesDynamicFuncs inspects the AST of a compiled template (and all associated templates)
// to check if it calls any context-dependent functions that must be rebound at execution time.
func templateUsesDynamicFuncs(tmpl *template.Template) bool {
	if tmpl == nil {
		return false
	}
	for _, t := range tmpl.Templates() {
		if t.Tree != nil && hasDynamicFuncs(t.Root) {
			return true
		}
	}
	return false
}

func hasDynamicFuncs(node parse.Node) bool {
	if node == nil {
		return false
	}
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return false
		}
		for _, child := range n.Nodes {
			if hasDynamicFuncs(child) {
				return true
			}
		}
	case *parse.ActionNode:
		return hasDynamicFuncs(n.Pipe)
	case *parse.PipeNode:
		if n == nil {
			return false
		}
		for _, cmd := range n.Cmds {
			if hasDynamicFuncs(cmd) {
				return true
			}
		}
	case *parse.CommandNode:
		if n == nil {
			return false
		}
		for _, arg := range n.Args {
			if hasDynamicFuncs(arg) {
				return true
			}
		}
	case *parse.IdentifierNode:
		switch n.Ident {
		case "get", "tryGet", "entry", "search", "include", "includeFile", "exec":
			return true
		}
	case *parse.IfNode:
		return hasDynamicFuncs(n.Pipe) || hasDynamicFuncs(n.List) || hasDynamicFuncs(n.ElseList)
	case *parse.RangeNode:
		return hasDynamicFuncs(n.Pipe) || hasDynamicFuncs(n.List) || hasDynamicFuncs(n.ElseList)
	case *parse.WithNode:
		return hasDynamicFuncs(n.Pipe) || hasDynamicFuncs(n.List) || hasDynamicFuncs(n.ElseList)
	case *parse.TemplateNode:
		return hasDynamicFuncs(n.Pipe)
	case *parse.ChainNode:
		return hasDynamicFuncs(n.Node)
	}
	return false
}

// tmplCache is the package-level LRU cache for precompiled template ASTs.
var tmplCache = newTemplateLRUCache(defaultTemplateCacheCapacity)

// PurgeTemplateCache clears all entries from the template AST cache and resets hit/miss counters.
func PurgeTemplateCache() {
	tmplCache.Purge()
}

// TemplateCacheStats returns the current hit count, miss count, and number of cached template ASTs.
func TemplateCacheStats() (hits, misses uint64, size int) {
	hits, misses = tmplCache.Stats()
	size = tmplCache.Len()
	return hits, misses, size
}
