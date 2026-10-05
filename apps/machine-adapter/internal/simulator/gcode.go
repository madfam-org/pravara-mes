package simulator

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// GCodePath is a timed toolhead path parsed from G-code for the Moonraker
// simulator. It is test scaffolding: moves run at their programmed feed rate
// with no acceleration (Klipper plans trapezoidal moves), and G28 marks axes
// homed without moving them. Positions are X, Y, Z, E in mm in printer
// coordinates; nothing here models a machine's kinematics.
//
// Supported: G0/G1 (X Y Z E F), G90/G91 (absolute/relative XYZ and E),
// M82/M83 (absolute/relative E), G92 (set position) and G28 (home; axis
// letters select axes). Comments after ';' are ignored. Other commands are
// skipped and listed in Skipped.
type GCodePath struct {
	Start    [4]float64
	Segments []PathSegment
	// Homing lists, in order, the times at which axes become homed.
	Homing []HomingEvent
	// Skipped lists the commands the simulator does not model.
	Skipped []string
}

// PathSegment is one linear move.
type PathSegment struct {
	Line     int // 1-based G-code line
	From, To [4]float64
	Begin    time.Duration // offset from the start of the path
	Duration time.Duration
	// Speed is the XYZ speed in mm/s (0 for an extruder-only move), the value
	// Klipper reports as motion_report.live_velocity.
	Speed float64
}

// HomingEvent records G28 at a path time.
type HomingEvent struct {
	At   time.Duration
	Axes string // subset of "xyz"
}

// ParseGCodePath parses src starting from start. Every motion command needs
// a feed rate (F, modal) before the first move.
func ParseGCodePath(src []byte, start [4]float64) (*GCodePath, error) {
	p := &GCodePath{Start: start}
	pos := start
	var t time.Duration
	feed := 0.0 // mm/min
	relXYZ, relE := false, false
	sc := bufio.NewScanner(bytes.NewReader(src))
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if i := strings.IndexByte(text, ';'); i >= 0 {
			text = text[:i]
		}
		fields := strings.Fields(strings.ToUpper(text))
		if len(fields) == 0 {
			continue
		}
		cmd, words, err := splitWords(fields)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		switch cmd {
		case "G0", "G1":
			if f, ok := words['F']; ok {
				if f <= 0 {
					return nil, fmt.Errorf("line %d: feed rate must be positive", line)
				}
				feed = f
			}
			next := pos
			for i, axis := range []byte{'X', 'Y', 'Z', 'E'} {
				v, ok := words[axis]
				if !ok {
					continue
				}
				rel := relXYZ
				if axis == 'E' {
					rel = relE
				}
				if rel {
					next[i] = pos[i] + v
				} else {
					next[i] = v
				}
			}
			if next == pos {
				continue
			}
			if feed == 0 {
				return nil, fmt.Errorf("line %d: move before any feed rate", line)
			}
			xyz := math.Sqrt(sq(next[0]-pos[0]) + sq(next[1]-pos[1]) + sq(next[2]-pos[2]))
			dist, speed := xyz, feed/60
			if xyz == 0 { // extruder-only move: F applies to E
				dist = math.Abs(next[3] - pos[3])
			}
			dur := time.Duration(dist / speed * float64(time.Second))
			seg := PathSegment{Line: line, From: pos, To: next, Begin: t, Duration: dur}
			if xyz > 0 {
				seg.Speed = speed
			}
			p.Segments = append(p.Segments, seg)
			t += dur
			pos = next
		case "G90":
			relXYZ, relE = false, false
		case "G91":
			relXYZ, relE = true, true
		case "M82":
			relE = false
		case "M83":
			relE = true
		case "G92":
			for i, axis := range []byte{'X', 'Y', 'Z', 'E'} {
				if v, ok := words[axis]; ok {
					pos[i] = v
				}
			}
		case "G28":
			axes := ""
			for _, a := range []byte{'X', 'Y', 'Z'} {
				if _, ok := words[a]; ok {
					axes += strings.ToLower(string(a))
				}
			}
			if axes == "" {
				axes = "xyz"
			}
			p.Homing = append(p.Homing, HomingEvent{At: t, Axes: axes})
		default:
			p.Skipped = append(p.Skipped, cmd)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

func sq(v float64) float64 { return v * v }

// splitWords splits "G1 X10 Y-2.5" into "G1" and its letter words. A bare
// axis letter (as in "G28 X") has the value 0.
func splitWords(fields []string) (string, map[byte]float64, error) {
	cmd := fields[0]
	words := map[byte]float64{}
	for _, f := range fields[1:] {
		letter := f[0]
		if len(f) == 1 {
			words[letter] = 0
			continue
		}
		v, err := strconv.ParseFloat(f[1:], 64)
		if err != nil {
			return "", nil, fmt.Errorf("bad word %q", f)
		}
		words[letter] = v
	}
	return cmd, words, nil
}

// Duration is the total time of the path.
func (p *GCodePath) Duration() time.Duration {
	if len(p.Segments) == 0 {
		return 0
	}
	last := p.Segments[len(p.Segments)-1]
	return last.Begin + last.Duration
}

// End is the final position.
func (p *GCodePath) End() [4]float64 {
	if len(p.Segments) == 0 {
		return p.Start
	}
	return p.Segments[len(p.Segments)-1].To
}

// At returns the interpolated position, the XYZ speed and the end point of
// the move in progress (the commanded position) at time t into the path.
func (p *GCodePath) At(t time.Duration) (pos [4]float64, speed float64, commanded [4]float64) {
	if len(p.Segments) == 0 || t <= 0 {
		return p.Start, 0, p.Start
	}
	for _, s := range p.Segments {
		if t >= s.Begin+s.Duration {
			continue
		}
		f := 0.0
		if s.Duration > 0 {
			f = float64(t-s.Begin) / float64(s.Duration)
		}
		for i := range pos {
			pos[i] = s.From[i] + (s.To[i]-s.From[i])*f
		}
		return pos, s.Speed, s.To
	}
	end := p.End()
	return end, 0, end
}

// HomedAt returns the homed axes at time t, in "xyz" order.
func (p *GCodePath) HomedAt(t time.Duration) string {
	set := map[byte]bool{}
	for _, h := range p.Homing {
		if h.At <= t {
			for i := 0; i < len(h.Axes); i++ {
				set[h.Axes[i]] = true
			}
		}
	}
	out := ""
	for _, a := range []byte{'x', 'y', 'z'} {
		if set[a] {
			out += string(a)
		}
	}
	return out
}

// Locate returns the path parameter of pos: the segment index plus the
// fraction along it, for the first segment at or after from that contains pos
// within tol (mm). It lets a test check that observed positions follow the
// path in order.
func (p *GCodePath) Locate(pos [4]float64, from float64, tol float64) (float64, bool) {
	if len(p.Segments) == 0 {
		return 0, dist4(pos, p.Start) <= tol
	}
	startSeg := int(from)
	if startSeg < 0 {
		startSeg = 0
	}
	for i := startSeg; i < len(p.Segments); i++ {
		s := p.Segments[i]
		f, d := project(pos, s.From, s.To)
		if d <= tol {
			param := float64(i) + f
			if param+1e-9 >= from {
				return param, true
			}
		}
	}
	return 0, false
}

// project returns the clamped fraction of pos along a→b and the distance from it.
func project(pos, a, b [4]float64) (float64, float64) {
	var ab, ap [4]float64
	l2 := 0.0
	dot := 0.0
	for i := range pos {
		ab[i] = b[i] - a[i]
		ap[i] = pos[i] - a[i]
		l2 += ab[i] * ab[i]
		dot += ab[i] * ap[i]
	}
	f := 0.0
	if l2 > 0 {
		f = math.Max(0, math.Min(1, dot/l2))
	}
	var q [4]float64
	for i := range q {
		q[i] = a[i] + ab[i]*f
	}
	return f, dist4(pos, q)
}

func dist4(a, b [4]float64) float64 {
	s := 0.0
	for i := range a {
		s += sq(a[i] - b[i])
	}
	return math.Sqrt(s)
}
