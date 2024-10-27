[![GoDoc](https://godoc.org/github.com/diegommm/adaptivepool?status.svg)](https://godoc.org/github.com/diegommm/adaptivepool)
[![codecov](https://codecov.io/github/diegommm/adaptivepool/graph/badge.svg?token=MT4Z39DA23)](https://codecov.io/github/diegommm/adaptivepool)
[![Go Report Card](https://goreportcard.com/badge/github.com/diegommm/adaptivepool)](https://goreportcard.com/report/github.com/diegommm/adaptivepool)
[![Codacy Badge](https://app.codacy.com/project/badge/Grade/2a3bd79d8ed64cb5bd30ffae9e4f0486)](https://app.codacy.com/gh/diegommm/adaptivepool/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_grade)

# Stochastic free list based on sync.Pool

Package **adaptivepool** provides a free list based on sync.Pool that can
stochastically define which items should be reused, based on a measure of
choice called cost.

Example usage of AdaptivePool:

```go
// pool holds *bytes.Buffer items for reuse
var pool = adaptivepool.New(
    adaptivepool.BytesBufferProvider{},
    adaptivepool.NormalEstimator{
        Threshold: 2, // reuse buffer if its Len is in Mean ± 2 * StdDev
        MinCap: 512,  // minimum capacity of newly created items
    },
    500, // bias towards the latest 500 elements to increase adaptability
)

func postJSON(url string, jsonBody any) (*http.Response, error) {
    buf := pool.Get()
    defer pool.Put(buf)
    if err := json.NewEncoder(buf).Encode(); err != nil {
        return nil, fmt.Errorf("encode JSON body: %w", err)
    }
    return http.Post(url, "application/json", buf)
}
```

Example usage of `ReaderBufferer`:

```go
// bufferAndClose is a an http.Handler decorator that ensures that request
// bodies are read and closed fully as soon as possible. Once `Close` is called
// on the replaced Body, the internal buffer will be transparently released and
// could potentially be reused.
func bufferAndClose(next http.Handler) http.Handler {
    bodiesPool := adaptivepool.NewReaderBufferer(
        adaptivepool.NormalEstimator{
            Threshold: 2, // reuse buffer if its Len is in Mean ± 2 * StdDev
            MinCap: 512,  // minimum capacity of newly created items
        },
        500, // bias towards the latest 500 elements to increase adaptability
    )

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        rc, err := bodiesPool.ReadCloserWithCost(r.Body, int(r.ContentLength))
        if err != nil {
            w.WriteHeader(http.StatusInternalServerError)
            log.Printf("buffer request: %v", err)
            return
        }
        req.Body = rc
        return next.ServeHTTP(req)
    })
}
```

## AdaptivePool

The API is very similar to that of [sync.Pool], but it uses specific types
instead of `any`. It keeps a basic set of online statistics about the cost of
items `Put` in the pool. An additional parameter provided during creation,
`maxN`, allows to increase the adaptability of the system to seasonal changes.

The implementation delegates type-specific operations like measuring the cost of
an item or creating an item with a specific cost to the `ItemProvider`
interface; and the reuse policy and estimation of the cost of new items to the
`Estimator` interface. Two implementations for `ItemProvider` are given: a
generic one for slices and one for `*bytes.Buffer`. Both have a similar
treatment of the items, clearing all bytes before putting them back into the
pool. This is to prevent accidentally leaking confidential data into other uses,
but can be disabled by making a new implementation, which should be trivial. The
`NormalEstimator` implementation of `Estimator` will discard items with a cost
outside of the inclusive range `Mean ± Threshold * StdDev`, and newly created
items will have a preallocated cost of `Mean + Threshold * StdDev`, and with a
minimum cost of `MinCap`.

## Running tests and benchmarks

For a quick run, try:

```shell
go test -short -race -cover ./...
```

For the full suite, which includes testing against randomly generated data, try:

```shell
go test -race -cover ./...
```

To run benchmarks:

```shell
go test -run=- -count=20 | benchstat -col=/implem -
```
