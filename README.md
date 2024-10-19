# Stochastic free list based on sync.Pool

Package **adaptivepool** provides a free list based on sync.Pool that can
stochastically define which items should be reused, based on a measure of
choice called size.

Example usage:

```go
// pool holds *bytes.Buffer items for reuse. Items considered outliers will be
// dropped so they are garbage collected. Items in the range Mean ± 2 * StdDev
// will be put back in the pool to be available for reuse. We will bias towards
// the latest 500 items observed so as to adapt faster to traffic changes.
var pool = adaptivepool.New(adaptivepool.NormalBytesBuffer{2}, 500)

func postJSON(url string, jsonBody any) (*http.Response, error) {
    buf := pool.Get()
    defer pool.Put(buf)
    if err := json.NewEncoder(buf).Encode(); err != nil {
        return nil, fmt.Errorf("encode JSON body: %w", err)
    }
    return http.Post(url, "application/json", buf)
}
```

The API is very similar to that of [sync.Pool], but it uses specific types
instead of `any`. It uses a basic rolling statistics implementation to keep
track of the number of items `Put` in the pool, the Mean, and Standard Deviation
of their measured size. An additional parameter provided during creation,
`maxN`, allows to increase the adaptability of the system to seasonal changes.
Following the example above, if the length of the encoded JSON bodies POSTed
would increase during an application-specific flow, then the statistics could
potentially cause a lag in adapting to this change. A fair value for `maxN`
(which depends on the application) compensates the resistance of statistics of
large populations (e.g. long running programs, like servers or scrappers),
allowing for faster adaptation to changes.

The implementation decouples both type-specific operations as well as the
decision on when an item is elligible for reuse with the `PoolItemProvider`
interface. Two implementations are provided: one for slices of any type and one
for `*bytes.Buffer`. Both have a similar treatment of the items, considering
that their length follows a Normal Distribution, and only items in the specified
number of Standard Deviations away from the Mean will be elligible for reuse.
All other items are considered outliers and are left for garbage collection,
which makes a more effective use of the internal `sync.Pool`.

The fact that the measures of Mean and Standard Deviation are permanently
updated allows the system to adapt to changing conditions. It also removes the
need to establish hardcoded global limits on when to reuse allocated items in a
regular `sync.Pool`.

The parameter `maxN` could also be named 'adaptation window', and a good rule of
thumb to choose a starting value is thinking how many observations it would take
to make a reasonably accurate new estimation of statistical parameters after a
change in their distribution in your application.
