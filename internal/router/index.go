package router

// M7 — polynomial rolling hash for static-segment lookup.
//
// segmentIndex is the seam that makes the hash swappable against a plain
// map (LEARNING.md M7: "keep it swappable, and benchmark it against the
// map — that benchmark is the whole justification story").

// segmentIndex maps a static segment to its child node.
type segmentIndex interface {
	get(seg string) (*node, bool)
	set(seg string, n *node)
}

// --- map-backed index (the boring, correct baseline) ---

type mapIndex map[string]*node

func (m mapIndex) get(seg string) (*node, bool) {
	n, ok := m[seg]
	return n, ok
}

func (m mapIndex) set(seg string, n *node) {
	m[seg] = n
}


const (
	hashPrime = 31
	hashMod = 1_000_000_007
)


func segmentHash(s string) uint64 {
	var h uint64
	for i := 0; i < len(s); i++ {
		h = (h*hashPrime + uint64(s[i])) % hashMod
	}
	return h
}


type hashEntry struct {
	used bool
	hash uint64
	seg  string
	n    *node
}


type hashIndex struct {
	entries []hashEntry
	count   int
}

func (hi *hashIndex) get(seg string) (*node, bool) {
	if hi.count == 0 {
		return nil, false
	}

	h := segmentHash(seg)
	mask := uint64(len(hi.entries) - 1)
	for i := h & mask; ; i = (i + 1) & mask {
		e := &hi.entries[i]
		if !e.used {
			return nil, false
		}
		if e.hash == h && e.seg == seg {
			return e.n, true
		}
	}
}

func (hi *hashIndex) set(seg string, n *node) {
	if len(hi.entries) == 0 {
		hi.entries = make([]hashEntry, 8)
	}

	if (hi.count+1)*4 > len(hi.entries)*3 {
		hi.grow()
	}
	hi.insert(segmentHash(seg), seg, n)
}

func (hi *hashIndex) insert(h uint64, seg string, n *node) {
	mask := uint64(len(hi.entries) - 1)
	for i := h & mask; ; i = (i + 1) & mask {
		e := &hi.entries[i]
		if !e.used {
			*e = hashEntry{used: true, hash: h, seg: seg, n: n}
			hi.count++
			return
		}
		if e.hash == h && e.seg == seg {
			e.n = n
			return
		}
	}
}

func (hi *hashIndex) grow() {
	old := hi.entries
	hi.entries = make([]hashEntry, len(old)*2)
	hi.count = 0
	for i := range old {
		if old[i].used {
			hi.insert(old[i].hash, old[i].seg, old[i].n)
		}
	}
}
