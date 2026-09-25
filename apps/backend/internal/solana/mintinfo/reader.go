package mintinfo

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Reader loads mint extension metadata.
type Reader interface {
	Info(ctx context.Context, mint string) (Info, error)
}

// HTTPReader fetches mint accounts from Solana JSON-RPC with caching and fallbacks.
type HTTPReader struct {
	rpcURL string
	rpc    *rpcClient
	cache  *cacheStore
	now    func() time.Time
}

// NewHTTPReader builds a reader for rpcURL. Empty rpcURL skips RPC and uses static fallback only.
func NewHTTPReader(rpcURL string) *HTTPReader {
	return NewHTTPReaderWithClient(rpcURL, nil, time.Now)
}

// NewHTTPReaderWithClient injects HTTP client and clock for tests.
func NewHTTPReaderWithClient(rpcURL string, httpClient *http.Client, now func() time.Time) *HTTPReader {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	rpcURL = strings.TrimSpace(rpcURL)
	r := &HTTPReader{
		rpcURL: rpcURL,
		now:    now,
		cache:  newCacheStore(now),
	}
	if rpcURL != "" {
		r.rpc = &rpcClient{url: rpcURL, client: httpClient}
	}
	return r
}

// Info returns mint metadata, using cache, RPC, last-good, and static fallback tiers.
func (r *HTTPReader) Info(ctx context.Context, mint string) (Info, error) {
	mint = strings.TrimSpace(mint)
	if mint == "" {
		return Info{}, ErrUnknownMint
	}

	if info, ok := r.cache.getCached(mint); ok {
		return info, nil
	}

	if r.rpc == nil {
		return r.staticOrUnknown(mint)
	}

	info, err := r.fetchFromRPC(ctx, mint)
	if err == nil {
		return info, nil
	}

	now := r.now()
	if lg, ok := r.cache.getLastGood(mint); ok {
		slog.Warn("mintinfo fallback", "mint", mint, "tier", "last-good", "err", err)
		lg.FetchedAt = now
		lg = mergePauseFailOpen(lg, lg, true)
		lg = applyStaticIfZero(lg, mint, now)
		return lg, nil
	}

	if static, ok := StaticFallback(mint, now); ok {
		slog.Warn("mintinfo fallback", "mint", mint, "tier", "static", "err", err)
		return static, nil
	}
	return Info{}, ErrUnknownMint
}

func (r *HTTPReader) staticOrUnknown(mint string) (Info, error) {
	now := r.now()
	if info, ok := StaticFallback(mint, now); ok {
		return info, nil
	}
	return Info{}, ErrUnknownMint
}

func (r *HTTPReader) fetchFromRPC(ctx context.Context, mint string) (Info, error) {
	epoch, err := r.currentEpoch(ctx)
	if err != nil {
		return Info{}, err
	}

	body, err := r.rpc.getAccountInfo(ctx, mint)
	if err != nil {
		return Info{}, err
	}

	now := r.now()
	info, err := parseAccountInfo(body, mint, epoch, now)
	if err != nil {
		return Info{}, err
	}

	effectiveTs := multiplierEffectiveTimestamp(body)
	usedNew := effectiveTs != 0 && now.Unix() >= effectiveTs

	info = applyStaticIfZero(info, mint, now)

	r.cache.putMint(mint, info, effectiveTs, usedNew)
	return info, nil
}

func (r *HTTPReader) currentEpoch(ctx context.Context) (uint64, error) {
	if epoch, ok := r.cache.getEpoch(); ok {
		return epoch, nil
	}
	body, err := r.rpc.getEpochInfo(ctx)
	if err != nil {
		return 0, err
	}
	epoch, err := parseEpochInfo(body)
	if err != nil {
		return 0, err
	}
	r.cache.putEpoch(epoch)
	return epoch, nil
}
