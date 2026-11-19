package library

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// BatchFanOut executes fn across all items concurrently using up to maxConcurrency workers
// (defaulting to runtime.NumCPU()) and returns results in the exact same input order.
func BatchFanOut[In, Out any](ctx context.Context, items []In, fn func(context.Context, In) (Out, error), maxConcurrency ...int) ([]Out, error) {
	n := len(items)
	if n == 0 {
		return []Out{}, nil
	}

	concurrency := runtime.NumCPU()
	if len(maxConcurrency) > 0 && maxConcurrency[0] > 0 {
		concurrency = maxConcurrency[0]
	}
	if concurrency > n {
		concurrency = n
	}

	results := make([]Out, n)

	// Work channel populated with item indices
	workCh := make(chan int, n)
	for i := 0; i < n; i++ {
		workCh <- i
	}
	close(workCh)

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var firstErr error
	var errOnce sync.Once

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case idx, ok := <-workCh:
					if !ok {
						return
					}
					out, err := fn(ctx, items[idx])
					if err != nil {
						errOnce.Do(func() {
							firstErr = fmt.Errorf("item %d failed: %w", idx, err)
							cancel()
						})
						return
					}
					results[idx] = out
				}
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		return results, firstErr
	}

	return results, nil
}

// FanOut executes workerFn over all items concurrently across CPU cores
// and returns the results in exact input order.
func FanOut[In, Out any](ctx context.Context, items []In, workerFn func(In) Out, maxConcurrency ...int) []Out {
	res, _ := BatchFanOut(ctx, items, func(_ context.Context, item In) (Out, error) {
		return workerFn(item), nil
	}, maxConcurrency...)
	return res
}
