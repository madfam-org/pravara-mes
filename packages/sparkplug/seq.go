package sparkplug

import "sync"

// SeqCounter issues the per-edge-node message sequence number: 0 on NBIRTH,
// then +1 per message, wrapping from 255 to 0 (spec 3.0, "Sequence Number").
// It is safe for concurrent use.
type SeqCounter struct {
	mu   sync.Mutex
	next uint8
}

// Reset prepares the counter for a new NBIRTH; the next call to Next returns 0.
func (s *SeqCounter) Reset() {
	s.mu.Lock()
	s.next = 0
	s.mu.Unlock()
}

// Next returns the sequence number for the next message and advances the counter.
func (s *SeqCounter) Next() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.next
	s.next++ // uint8 wraps 255 -> 0
	return uint64(v)
}

// NextBdSeq returns the birth/death sequence number for the next MQTT CONNECT:
// previous + 1, wrapping from 255 to 0 (spec 3.0, "bdSeq").
func NextBdSeq(prev uint64) uint64 { return (prev + 1) % 256 }

// SeqFollows reports whether got is the sequence number expected after prev,
// for host-side gap detection.
func SeqFollows(prev, got uint64) bool { return got == (prev+1)%256 }
