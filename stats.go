package adaptivepool

import "math"

// Stats efficiently computes a set of statistical values of numbers pushed to
// it, with high precision, and without the need to store all the values.
type Stats struct {
	n, actualN, maxN float64
	oldM, newM       float64
	oldS, newS       float64
}

// Push adds a new value to the sample.
func (s *Stats) Push(v float64) {
	if s.n < s.maxN || s.maxN < 1 {
		s.n++
	}
	if s.actualN++; s.actualN > 1 {
		s.newM = math.FMA(s.oldM, s.n-1, v) / s.n
		s.newS = math.FMA(math.Abs(v-s.oldM), math.Abs(v-s.newM), s.oldS)
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

// MaxN returns the maximum value of N. See [*Stats.SetMaxN] for details.
func (s *Stats) MaxN() float64 { return math.Round(s.maxN) }

// SetMaxN will prevent the value of N (the number of observations) from being
// incremented beyond `maxN`. This is useful to keep a bias towards latest
// values, improving the adaptability to seasonal changes in data distribution.
// Using a value less than one disables this behaviour. If the current value of
// N is already higher, then it will be set to `maxN` immediately. A value too
// low may cause too much disturbance, while a value too high may reduce
// adaptability. A recommended starting value is 500, if your application can
// tolerate it, and probably no less than 100 otherwise.
func (s *Stats) SetMaxN(maxN float64) {
	if maxN < 1 {
		maxN = 0
	} else {
		maxN = math.Round(maxN)
	}
	s.maxN = maxN
	if s.maxN >= 1 && s.n > s.maxN {
		s.n = s.maxN
	}
}

// Mean returns the Arithmetic Mean of the pushed values.
func (s *Stats) Mean() float64 { return s.newM }

// StdDev returns the (Population) Standard Deviation of the pushed values. If
// less than 2 values were pushed, then NaN is returned.
func (s *Stats) StdDev() float64 {
	if s.actualN > 1 {
		return math.Sqrt(s.newS / s.actualN)
	}
	return math.NaN()
}
