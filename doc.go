// Package adaptivepool provides a free list based on [sync.Pool] that can
// stochastically define which items should be reused, based on a measure of
// choice called size. It also provides a bufferer for io.Reader and
// io.ReadCloser based on this free list.
package adaptivepool
