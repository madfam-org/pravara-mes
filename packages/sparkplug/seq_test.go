package sparkplug

import (
	"sync"
	"testing"
)

func TestSeqWrapsAt255(t *testing.T) {
	var s SeqCounter
	s.Reset()
	for want := 0; want < 600; want++ {
		if got := s.Next(); got != uint64(want%256) {
			t.Fatalf("message %d: seq %d", want, got)
		}
	}
	s.Reset()
	if s.Next() != 0 {
		t.Fatal("Reset must restart at 0 for NBIRTH")
	}
	if !SeqFollows(255, 0) || !SeqFollows(7, 8) || SeqFollows(7, 9) {
		t.Fatal("SeqFollows")
	}
}

func TestSeqConcurrentUnique(t *testing.T) {
	var s SeqCounter
	seen := make([]int, 256)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 64; i++ {
				v := s.Next()
				mu.Lock()
				seen[v]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for v, n := range seen {
		if n != 2 {
			t.Fatalf("seq %d issued %d times in 512 calls", v, n)
		}
	}
}

func TestBdSeqWraps(t *testing.T) {
	if NextBdSeq(0) != 1 || NextBdSeq(254) != 255 || NextBdSeq(255) != 0 {
		t.Fatal("bdSeq must increment and wrap 255 -> 0")
	}
}
