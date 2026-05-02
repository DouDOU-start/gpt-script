package runtime

import (
	"context"
	"sync"
	"time"
)

type BatchPolicy struct {
	BatchSize       int
	Workers         int
	DelayBetween    time.Duration
	StaggerInterval time.Duration
}

type BatchItemFunc[T any] func(ctx context.Context, index int, item T) error

func RunInBatches[T any](ctx context.Context, items []T, policy BatchPolicy, fn BatchItemFunc[T]) []error {
	if policy.BatchSize <= 0 || policy.BatchSize > len(items) {
		policy.BatchSize = len(items)
	}
	if policy.Workers <= 0 {
		policy.Workers = 1
	}
	errs := make([]error, len(items))
	for start := 0; start < len(items); start += policy.BatchSize {
		end := start + policy.BatchSize
		if end > len(items) {
			end = len(items)
		}
		runWindow(ctx, items, start, end, policy, fn, errs)
		if ctx.Err() != nil || end == len(items) {
			break
		}
		if policy.DelayBetween > 0 {
			select {
			case <-ctx.Done():
				return errs
			case <-time.After(policy.DelayBetween):
			}
		}
	}
	return errs
}

func runWindow[T any](ctx context.Context, items []T, start, end int, policy BatchPolicy, fn BatchItemFunc[T], errs []error) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < policy.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if ctx.Err() != nil {
					errs[idx] = ctx.Err()
					continue
				}
				errs[idx] = fn(ctx, idx, items[idx])
			}
		}()
	}
sendLoop:
	for idx := start; idx < end; idx++ {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- idx:
		}
		if policy.StaggerInterval > 0 && idx+1 < end {
			select {
			case <-ctx.Done():
				break sendLoop
			case <-time.After(policy.StaggerInterval):
			}
		}
	}
	close(jobs)
	wg.Wait()
}
