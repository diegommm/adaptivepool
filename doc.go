// Package adaptivepool provides a free list based on [sync.Pool] that can
// determine which items should be reused and how they should be pre-allocated
// based on a set of online stats of a measure of choice called cost. It also
// provides a bufferer for io.Reader and io.ReadCloser based on this free list.
// Unless stated otherwise, the documentation of sync.Pool is also valid for the
// implementation in this package.
package adaptivepool
