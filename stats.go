package adaptivepool

import "math"

type Stats = stats1

// stats1 is generally better than stats0. It is also an implementation of
// Welford's algorithm but with a revision to reduce computing error, originally
// presented by Knuth's Art of Computer Programming. This is the fastest
// alternative among those who provide the best accuracy.
// Source:
//
//	https://www.johndcook.com/blog/standard_deviation/
type stats1 struct {
	n, oldM, newM, oldS, newS float64
}

func (s *stats1) Push(v float64) {
	s.n++
	if s.n > 1 {
		s.newM = s.oldM + (v-s.oldM)/s.n
		s.newS = s.oldS + (v-s.oldM)*(v-s.newM)
		s.oldM = s.newM
		s.oldS = s.newS

	} else {
		s.oldM = v
		s.newM = v
	}
}

func (s *stats1) Reset() { *s = stats1{} }

func (s *stats1) Mean() float64 { return s.newM }

func (s *stats1) StdDev() float64 {
	if s.n > 1 {
		return math.Sqrt(s.newS / (s.n - 1))
	}
	return 0
}
