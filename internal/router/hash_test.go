// Tests for M7 — the polynomial rolling hash and the hash-backed index.
// See EDGE_CASES.md §M7: the most important test here is the engineered
// collision — hash equality must be confirmed by string compare.
package router

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashKnownAnswer(t *testing.T) {
	// Hand-computed: 'a'=97, 'b'=98, 'c'=99 with p=31, m=1e9+7:
	// ((97*31)+98)*31 + 99 = 96354. Guards against silent formula edits.
	assert.Equal(t, uint64(96354), segmentHash("abc"))
	assert.Equal(t, uint64(97), segmentHash("a"))
	assert.Equal(t, uint64(0), segmentHash(""))
}

func TestHashDeterministic(t *testing.T) {
	assert.Equal(t, segmentHash("users"), segmentHash("users"))
}

func TestHashNearMisses(t *testing.T) {
	// Order matters (that's the positional weighting p gives us).
	assert.NotEqual(t, segmentHash("ab"), segmentHash("ba"))
	// A trailing NUL still changes the hash: 97 vs 97*31+0.
	assert.NotEqual(t, segmentHash("a"), segmentHash("a\x00"))
}

func TestHashKnownDegeneracy(t *testing.T) {
	// "" and "\x00" collide by construction: h = h*p + 0 absorbs leading
	// NULs. Documented in index.go — this is why cp-algorithms maps
	// chars to values >= 1. Harmless here (URL parsing rejects NULs and
	// the table confirms strings), but the test keeps the fact visible.
	assert.Equal(t, segmentHash(""), segmentHash("\x00"))
}

func TestHashStaysBelowModulus(t *testing.T) {
	long := make([]byte, 1<<20)
	for i := range long {
		long[i] = byte(i)
	}
	assert.Less(t, segmentHash(string(long)), uint64(hashMod))
}

func TestHashSingleByteNoCollisions(t *testing.T) {
	seen := map[uint64]bool{}
	for b := 0; b < 256; b++ {
		h := segmentHash(string([]byte{byte(b)}))
		assert.False(t, seen[h], "single-byte collision at %d", b)
		seen[h] = true
	}
}

func randSegment(rng *rand.Rand) string {
	b := make([]byte, 8)
	for i := range b {
		b[i] = byte('a' + rng.Intn(26))
	}
	return string(b)
}

// TestHashCollisionStillResolves is the M7 test that matters: birthday-
// search for two different segments with the SAME hash (m = 1e9+7 needs
// only ~40k random strings), then prove the trie still routes each to
// its own handler — because the index confirms string equality on every
// hash hit. Correctness never rests on the hash alone.
func TestHashCollisionStillResolves(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	seen := map[uint64]string{}

	var a, b string
	for {
		s := randSegment(rng)
		h := segmentHash(s)
		if prev, ok := seen[h]; ok && prev != s {
			a, b = prev, s
			break
		}
		seen[h] = s
	}

	require.Equal(t, segmentHash(a), segmentHash(b), "search must yield a real collision")
	require.NotEqual(t, a, b)

	r := NewHashed()
	require.NoError(t, r.Register("GET", "/x/"+a, mark("handler-a")))
	require.NoError(t, r.Register("GET", "/x/"+b, mark("handler-b")))

	m, err := r.Lookup("GET", "/x/"+a)
	require.NoError(t, err)
	assert.Equal(t, "handler-a", called(t, m))

	m, err = r.Lookup("GET", "/x/"+b)
	require.NoError(t, err)
	assert.Equal(t, "handler-b", called(t, m))
}

func TestHashIndexGrowth(t *testing.T) {
	// Push one node's static index far past the initial table size so
	// grow() and its rehash run.
	r := NewHashed()
	for i := 0; i < 200; i++ {
		require.NoError(t, r.Register("GET", fmt.Sprintf("/seg%03d", i), mark(fmt.Sprintf("h%d", i))))
	}
	for i := 0; i < 200; i++ {
		m, err := r.Lookup("GET", fmt.Sprintf("/seg%03d", i))
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("h%d", i), called(t, m))
	}
}

// TestHashedMatchesMap is the differential test: both index backends run
// the same random route table and the same lookups, and must agree on
// every single answer — found or not, handler (identified by calling
// it), params.
func TestHashedMatchesMap(t *testing.T) {
	rng := rand.New(rand.NewSource(2))

	mapped := New()
	hashed := NewHashed()

	var patterns []string
	for i := 0; i < 200; i++ {
		var pattern string
		switch i % 4 {
		case 0:
			pattern = fmt.Sprintf("/%s/%s", randSegment(rng), randSegment(rng))
		case 1:
			pattern = fmt.Sprintf("/%s/:id", randSegment(rng))
		case 2:
			pattern = fmt.Sprintf("/%s/%s/%s", randSegment(rng), randSegment(rng), randSegment(rng))
		case 3:
			pattern = fmt.Sprintf("/%s/*rest", randSegment(rng))
		}
		name := fmt.Sprintf("h%d", i)

		errA := mapped.Register("GET", pattern, mark(name))
		errB := hashed.Register("GET", pattern, mark(name))
		require.Equal(t, errA == nil, errB == nil, "registration must agree for %q", pattern)
		if errA == nil {
			patterns = append(patterns, pattern)
		}
	}

	lookupOnce := func(path string) {
		ma, errA := mapped.Lookup("GET", path)
		mb, errB := hashed.Lookup("GET", path)
		require.Equal(t, errA, errB, "error mismatch for %q", path)
		if errA == nil {
			assert.Equal(t, called(t, ma), called(t, mb), "handler mismatch for %q", path)
			assert.Equal(t, ma.Params, mb.Params, "params mismatch for %q", path)
		}
	}

	// Hits: derive concrete paths from the registered patterns.
	for _, p := range patterns {
		path := strings.ReplaceAll(p, ":id", "42")
		path = strings.ReplaceAll(path, "*rest", "a/b/c")
		lookupOnce(path)
	}
	// Misses: random garbage.
	for i := 0; i < 300; i++ {
		lookupOnce(fmt.Sprintf("/%s/%s", randSegment(rng), randSegment(rng)))
	}
}
