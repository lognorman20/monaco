package trading_test

import (
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/modules/trading"
	"github.com/monaco/monaco/apps/backend/internal/platform/config"
	"github.com/monaco/monaco/apps/backend/internal/platform/httpx"
	"github.com/monaco/monaco/apps/backend/internal/platform/module"
)

func TestModule_isNamedTradingAndServesNothingYet(t *testing.T) {
	t.Parallel()
	m := trading.New(module.Deps{Config: config.Config{
		Solana: config.Solana{RPCURL: "http://fakes/rpc/"}, Timeouts: config.Timeouts{RPC: time.Second},
	}})
	var routes httpx.Routes
	m.Routes(&routes)
	if m.Name() != "trading" || len(m.Consumers()) != 0 || len(m.Pollers()) != 1 || routes != (httpx.Routes{}) {
		t.Fatalf("module = %s, %d consumers, %v pollers, routes %+v", m.Name(), len(m.Consumers()), m.Pollers(), routes)
	}
}
