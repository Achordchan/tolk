package inputassist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheKey identifies a translation. The engine fingerprint comes from Tolk
// (it has the model and endpoint of an AI engine, never a key), so switching
// engine or model stops the old entries from matching.
type CacheKey struct {
	Text, Source, Target, Engine string
}

func (k CacheKey) storage() string {
	return k.Engine + "\x1f" + k.Source + "\x1f" + k.Target + "\x1f" + k.Text
}

type cacheEntry struct {
	Key       string    `json:"key"`
	Text      string    `json:"translatedText"`
	CreatedAt time.Time `json:"createdAt"`
	Used      uint64    `json:"lastUsedSequence"`
}

func (e cacheEntry) size() int { return len(e.Key) + len(e.Text) + 32 }

// Cache is an LRU of translations, kept in memory and saved to a file a moment
// after it changes. Reads never touch the disk once loaded.
type Cache struct {
	mu       sync.Mutex
	entries  map[string]cacheEntry
	bytes    int
	seq      uint64
	maxBytes int
	maxCount int

	path    string
	loaded  bool
	dirty   bool
	flushIn time.Duration
	timer   *time.Timer
}

// NewCache makes a cache saved at path ("" keeps it in memory only).
func NewCache(path string) *Cache {
	return &Cache{
		entries: map[string]cacheEntry{}, maxBytes: 8 << 20, maxCount: 5000,
		path: path, flushIn: time.Second,
	}
}

// DefaultCachePath is Tolk\InputAssistTranslationCache.json in the user's
// cache directory. It is a cache: losing it only costs a few ⚡.
func DefaultCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Tolk", "InputAssistTranslationCache.json")
}

func (c *Cache) load() {
	if c.loaded {
		return
	}
	c.loaded = true
	if c.path == "" {
		return
	}
	b, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	var list []cacheEntry
	if err := json.Unmarshal(b, &list); err != nil {
		_ = os.Remove(c.path) // a damaged file is just an empty cache
		return
	}
	c.entries, c.bytes, c.seq = map[string]cacheEntry{}, 0, 0
	for _, e := range list {
		c.entries[e.Key] = e
		c.seq = max(c.seq, e.Used)
	}
	for _, e := range c.entries {
		c.bytes += e.size()
	}
	c.evict()
}

// Get returns the cached translation, marking it as just used.
func (c *Cache) Get(k CacheKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	e, ok := c.entries[k.storage()]
	if !ok {
		return "", false
	}
	c.seq++
	e.Used = c.seq
	c.entries[e.Key] = e
	c.markDirty() // the use order is saved too, or evictions after a restart would be wrong
	return e.Text, true
}

// Put stores a translation. Blank ones are not worth keeping.
func (c *Cache) Put(k CacheKey, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	c.seq++
	e := cacheEntry{Key: k.storage(), Text: text, CreatedAt: time.Now(), Used: c.seq}
	if old, ok := c.entries[e.Key]; ok {
		c.bytes -= old.size()
	}
	c.entries[e.Key] = e
	c.bytes += e.size()
	c.evict()
	c.markDirty()
}

// Len is the number of entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	return len(c.entries)
}

func (c *Cache) sorted() []cacheEntry {
	out := make([]cacheEntry, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Used < out[j].Used })
	return out
}

func (c *Cache) evict() {
	if len(c.entries) <= c.maxCount && c.bytes <= c.maxBytes {
		return
	}
	for _, e := range c.sorted() {
		if len(c.entries) <= c.maxCount && c.bytes <= c.maxBytes {
			return
		}
		delete(c.entries, e.Key)
		c.bytes -= e.size()
	}
}

func (c *Cache) markDirty() {
	c.dirty = true
	if c.path == "" || c.timer != nil {
		return
	}
	c.timer = time.AfterFunc(c.flushIn, func() {
		c.mu.Lock()
		c.timer = nil
		c.mu.Unlock()
		c.Flush()
	})
}

// Flush writes the cache to its file now if it changed.
func (c *Cache) Flush() {
	c.mu.Lock()
	if !c.dirty || c.path == "" {
		c.mu.Unlock()
		return
	}
	c.dirty = false
	list := c.sorted()
	c.mu.Unlock()
	b, err := json.Marshal(list)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(c.path), 0o755)
	}
	if err == nil {
		tmp := c.path + ".tmp"
		if err = os.WriteFile(tmp, b, 0o600); err == nil {
			if err = os.Rename(tmp, c.path); err != nil {
				_ = os.Remove(tmp)
			}
		}
	}
	if err != nil { // try again on the next change
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
	}
}
