// Package pyth serves the display side of the stock screens from Pyth: chart
// history (Benchmarks, with the Hermes sampler as fallback), the stats grid, the
// underlying's day change and its latest equity price as a reference line. It is
// never used for NAV; pot valuation and the hero price are Chainlink total-return
// marks (internal/chainlink). Mobile never calls Pyth; only this backend adapter does.
package pyth
