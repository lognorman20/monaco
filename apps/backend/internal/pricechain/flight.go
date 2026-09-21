package pricechain

import (
	"context"
	"sync"
)

// flightGroup collapses concurrent calls for the same key into one execution whose
// result every caller shares.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	done   chan struct{}
	result any
}

// do waits for the shared call however long it takes. Use it only where the
// caller has no deadline of its own to honour.
func (g *flightGroup) do(key string, fn func() any) any {
	call, leader := g.enter(key)
	if leader {
		go g.run(key, call, fn)
	}
	<-call.done
	return call.result
}

// doCtx is do, bounded by the caller's context.
//
// The shared call always runs to completion, whatever any one caller does: it is
// the thing that fills the cache, and abandoning it would mean a page of rows with
// short budgets never warms anything. What ctx bounds is how long *this* caller
// waits for it. Without that, a 1.2s budget upstream was decoration — the wait was
// a bare `<-call.done`, so a 20s fetch made every caller wait 20s no matter what
// deadline they arrived with.
func (g *flightGroup) doCtx(ctx context.Context, key string, fn func() any) (any, error) {
	call, leader := g.enter(key)
	if leader {
		go g.run(key, call, fn)
	}
	select {
	case <-call.done:
		return call.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// enter registers this caller against key, reporting whether it is the one that
// must run the work.
func (g *flightGroup) enter(key string) (*flightCall, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.calls == nil {
		g.calls = make(map[string]*flightCall)
	}
	if call, ok := g.calls[key]; ok {
		return call, false
	}
	call := &flightCall{done: make(chan struct{})}
	g.calls[key] = call
	return call, true
}

func (g *flightGroup) run(key string, call *flightCall, fn func() any) {
	defer func() {
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		close(call.done)
	}()
	call.result = fn()
}

// inFlight reports whether a shared call for key is already running, so a caller
// that only wants to trigger a warm does not start a second one.
func (g *flightGroup) inFlight(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.calls[key]
	return ok
}
