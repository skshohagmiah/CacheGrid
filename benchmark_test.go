package cachegrid

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func newBenchCache(b *testing.B) *Cache {
	b.Helper()
	c, err := New(Config{
		NumShards:       256,
		SweeperInterval: time.Hour, // disable sweeper noise
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { c.Shutdown() })
	return c
}

func BenchmarkSet(b *testing.B) {
	c := newBenchCache(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("key:%d", i), "value", 5*time.Minute)
	}
}

func BenchmarkGet(b *testing.B) {
	c := newBenchCache(b)
	for i := 0; i < 10000; i++ {
		c.Set(fmt.Sprintf("key:%d", i), "value", 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var val string
		c.Get(fmt.Sprintf("key:%d", i%10000), &val)
	}
}

func BenchmarkGetHit(b *testing.B) {
	c := newBenchCache(b)
	c.Set("fixed-key", "value", 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var val string
		c.Get("fixed-key", &val)
	}
}

func BenchmarkGetMiss(b *testing.B) {
	c := newBenchCache(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var val string
		c.Get("nonexistent", &val)
	}
}

func BenchmarkSetGet(b *testing.B) {
	c := newBenchCache(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key:%d", i)
		c.Set(key, i, 5*time.Minute)
		var val int
		c.Get(key, &val)
	}
}

func BenchmarkExists(b *testing.B) {
	c := newBenchCache(b)
	for i := 0; i < 10000; i++ {
		c.Set(fmt.Sprintf("key:%d", i), "value", 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Exists(fmt.Sprintf("key:%d", i%10000))
	}
}

func BenchmarkDelete(b *testing.B) {
	c := newBenchCache(b)
	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("key:%d", i), "value", 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Delete(fmt.Sprintf("key:%d", i))
	}
}

func BenchmarkIncr(b *testing.B) {
	c := newBenchCache(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Incr("counter", 1)
	}
}

func BenchmarkMSet(b *testing.B) {
	c := newBenchCache(b)
	items := make(map[string]Item, 10)
	for i := 0; i < 10; i++ {
		items[fmt.Sprintf("key:%d", i)] = Item{Value: "value", TTL: 5 * time.Minute}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.MSet(items)
	}
}

func BenchmarkMGet(b *testing.B) {
	c := newBenchCache(b)
	keys := make([]string, 10)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key:%d", i)
		keys[i] = key
		c.Set(key, "value", 0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.MGet(keys...)
	}
}

func BenchmarkGetOrSet(b *testing.B) {
	c := newBenchCache(b)
	fn := func() (interface{}, error) { return "computed", nil }

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var val string
		c.GetOrSet(fmt.Sprintf("key:%d", i%1000), &val, 5*time.Minute, fn)
	}
}

func BenchmarkConcurrentRead(b *testing.B) {
	c := newBenchCache(b)
	for i := 0; i < 10000; i++ {
		c.Set(fmt.Sprintf("key:%d", i), "value", 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			var val string
			c.Get(fmt.Sprintf("key:%d", r.Intn(10000)), &val)
		}
	})
}

func BenchmarkConcurrentWrite(b *testing.B) {
	c := newBenchCache(b)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			c.Set(fmt.Sprintf("key:%d", r.Intn(10000)), "value", 5*time.Minute)
		}
	})
}

func BenchmarkConcurrentMixed(b *testing.B) {
	c := newBenchCache(b)
	for i := 0; i < 10000; i++ {
		c.Set(fmt.Sprintf("key:%d", i), "value", 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(rand.Int63()))
		for pb.Next() {
			key := fmt.Sprintf("key:%d", r.Intn(10000))
			if r.Intn(10) < 7 { // 70% reads, 30% writes
				var val string
				c.Get(key, &val)
			} else {
				c.Set(key, "newvalue", 5*time.Minute)
			}
		}
	})
}
