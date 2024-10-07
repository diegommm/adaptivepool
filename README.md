# Stochastic free list based on sync.Pool

Package **adaptivepool** provides a free list based on sync.Pool that can
stochastically define which items should be reused, based on a measure of
choice, called size.

```go
// pool holds *bytes.Buffer items for reuse. Items considered outliers will be
// dropped so they are garbage collected. Items in the range Mean ± 2 * StdDev
// will be put back in the pool to be available for reuse.
var pool = adaptivepool.New(adaptivepool.NormalBytesBuffer{2})

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
of their measured size.

The implementation decouples both type-specific operations as well as the
decision on when an item is elligible for reuse with the `PoolItemProvider`
interface. Two implementations are provided: one for slices of any type and one
for `*bytes.Buffer`. Both have a similar treatment of the items, considering
that their length follows a Normal Distribution, and only items in the specified
number of Standard Deviations away from the Mean will be elligible for reuse.
All other items are considered outliers and are left for garbage collection,
which makes a more effective use of the internal `sync.Pool`.

The fact that the measures of Mean and Standard Deviation are permanently
updated allows the system to adapt to changing conditions, and to increase or
decrease what an 'outlier' is. It also removes the need to establish hardcoded
global limits on when to reuse allocated items in a regular `sync.Pool`.

## Considerations about allocation strategies in Go

Under many circumstances you will not have to use any type of pool or free list,
and you can very well just call `new` or a similar mechanism to allocate a new
object and pass it around your program, and it will live in the heap until it
needs to be garbage collected.

The Go garbage collector does a great job (and each time better) at releasing
unused memory, but under high pressure it requires more resources and longer
global synchronization. Thus, it is possible that you can benefit from using a
`sync.Pool` for tasks where you know you will have bursty allocations of a
certain value type, for a set short period of time, and then discard that
allocated object. In such scenarios, using a `sync.Pool` can reduce the number
of new objects that will need to be allocated by reusing them, thus relieving
the garbage collector from having to track that many objects.

But using `sync.Pool` without further considerations can be detrimental, since
within that burst, the need to allocate a single big object can potentially lead
to that big allocation being kept around for very little value. Reusing a single
object, but very big compared to what it might be needed when reused can be
detrimental to memory-efficiency. Thus, it is very common that for objects of
the same type but variable allocation size a fixed cutoff value is chosen so
that we don't put it back to the pool, and instead we allow it to be garbage
collected. Keeping like-sized objects circulating in the `sync.Pool` is a great
use of it. See the file `print.go` in the standard library's `fmt` package for a
great example of how to properly use `sync.Pool`, and
https://github.com/golang/go/issues/23199 for an excellent discussion around it.

The size of the allocated object is not everything, though. You may also want to
consider how it is structured, so we would speak of memory-cost rather than bare
allocation size in bytes or words. A single `struct` composed of 100 allocated
`*int` (roughly 200 words) is more memory costly than a single `[]int` with
`cap` 200 (also roughly 200 words).


// To be continued...
