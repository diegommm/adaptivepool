// Package adaptivepool provides a free list based on [sync.Pool] that can
// stochastically define which items should be reused, based on a measure of
// choice called cost. It also provides a bufferer for io.Reader and
// io.ReadCloser based on this free list. Unless stated otherwise, the
// documentation of sync.Pool is also valid for the implementation in this
// package.
package adaptivepool
