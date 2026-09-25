package clawpump

import (
	"context"
	"sync"
)

// Call is one tool invocation recorded by Fake.
type Call struct {
	Tool         string
	OperatorKey  string
	Address      string
	AmountMicros int64
}

// Fake records tool calls and never reaches ClawPump.
type Fake struct {
	mu    sync.Mutex
	calls []Call
	err   error
}

// NewFake returns a fake whose tools succeed.
func NewFake() *Fake { return &Fake{} }

// Fail makes every tool call return err (nil clears it).
func (f *Fake) Fail(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

// Calls returns a copy of the recorded calls in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

func (f *Fake) SetExternalWallet(ctx context.Context, operatorKey, solanaAddress string) error {
	_ = ctx
	return f.record(Call{Tool: ToolSetExternalWallet, OperatorKey: operatorKey, Address: solanaAddress})
}

func (f *Fake) AgentSend(ctx context.Context, operatorKey, toAddress string, amountMicros int64) error {
	_ = ctx
	return f.record(Call{Tool: ToolAgentSend, OperatorKey: operatorKey, Address: toAddress, AmountMicros: amountMicros})
}

func (f *Fake) record(call Call) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.err
}

var _ Client = (*Fake)(nil)
