package adaptivepool

import "math"

// Stats efficiently computes a set of statistical values of numbers pushed to
// it, with high precision, and without the need to store all the values.
type Stats struct {
	n, oldM, newM, oldS, newS float64
}

// Push adds a new value to the sample.
func (s *Stats) Push(v float64) {
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

// Reset clears all the data.
func (s *Stats) Reset() { *s = Stats{} }

// N returns the number of pushed values.
func (s *Stats) N() float64 { return s.n }

// Mean returns the Arithmetic Mean of the pushed values.
func (s *Stats) Mean() float64 { return s.newM }

// StdDev returns the (Population) Standard Deviation of the pushed values. If
// less than 2 values were pushed, then NaN is returned.
func (s *Stats) StdDev() float64 {
	if s.n > 1 {
		return math.Sqrt(s.newS / s.n)
	}
	return math.NaN()
}
