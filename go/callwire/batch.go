package callwire

import (
	"context"
	"sync"
)

// BatchCall describes one call in a batch.
type BatchCall struct {
	// Func is the remote function name.
	Func string
	// Args is the argument list (same as Import).  nil is treated as no args.
	Args []interface{}
}

// BatchResult holds the outcome of a single call within a batch.
type BatchResult struct {
	// Result is the raw decoded response.  nil when Err is non-nil.
	Result interface{}
	// Err is non-nil when the call failed (wire error, transport error, or
	// context cancellation).
	Err error
}

// Batch fires all calls concurrently against c and blocks until every call has
// completed or ctx is cancelled.  Results are returned in the same order as
// calls.  A context cancellation causes all pending calls to return ctx.Err()
// as their Err; already-completed calls keep their results.
//
// Example:
//
//	results := callwire.Batch(c, ctx, []callwire.BatchCall{
//	    {Func: "add",  Args: []interface{}{1, 2}},
//	    {Func: "echo", Args: []interface{}{"hi"}},
//	})
//	fmt.Println(results[0].Result, results[1].Result) // 3  hi
func Batch(c *Client, ctx context.Context, calls []BatchCall) []BatchResult {
	results := make([]BatchResult, len(calls))
	var wg sync.WaitGroup
	wg.Add(len(calls))
	for i, call := range calls {
		i, call := i, call
		go func() {
			defer wg.Done()
			res, err := Import[interface{}](c, ctx, call.Func, call.Args)
			results[i] = BatchResult{Result: res, Err: err}
		}()
	}
	wg.Wait()
	return results
}

// BatchT fires all calls concurrently and decodes each result into type T.
// It is the typed counterpart of Batch, suitable for homogeneous batches where
// every call returns the same concrete type (e.g., []float64 inference results).
//
// Example:
//
//	scores, errs := callwire.BatchT[float64](c, ctx, []callwire.BatchCall{
//	    {Func: "score", Args: []interface{}{"a"}},
//	    {Func: "score", Args: []interface{}{"b"}},
//	})
func BatchT[T any](c *Client, ctx context.Context, calls []BatchCall) ([]T, []error) {
	values := make([]T, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	wg.Add(len(calls))
	for i, call := range calls {
		i, call := i, call
		go func() {
			defer wg.Done()
			v, err := Import[T](c, ctx, call.Func, call.Args)
			values[i] = v
			errs[i] = err
		}()
	}
	wg.Wait()
	return values, errs
}

// BatchRef returns a function that, when called, fires the given calls as a
// batch using the default client (configured via Configure/CALLWIRE_HOST).
//
// Example:
//
//	batchScore := callwire.BatchRef("score")
//	results := batchScore(ctx, []callwire.BatchCall{{Args: []interface{}{"x"}}})
func BatchRef(funcName string) func(ctx context.Context, argSets [][]interface{}) []BatchResult {
	return func(ctx context.Context, argSets [][]interface{}) []BatchResult {
		calls := make([]BatchCall, len(argSets))
		for i, args := range argSets {
			calls[i] = BatchCall{Func: funcName, Args: args}
		}
		c, err := getDefaultClient()
		if err != nil {
			results := make([]BatchResult, len(calls))
			for i := range results {
				results[i].Err = err
			}
			return results
		}
		return Batch(c, ctx, calls)
	}
}
