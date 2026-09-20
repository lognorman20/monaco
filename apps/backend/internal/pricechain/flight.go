package pricechain

import "sync"

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

func (g *flightGroup) do(key string, fn func() any) any {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*flightCall)
	}
	if call, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-call.done
		return call.result
	}
	call := &flightCall{done: make(chan struct{})}
	g.calls[key] = call
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		close(call.done)
	}()
	call.result = fn()
	return call.result
}
