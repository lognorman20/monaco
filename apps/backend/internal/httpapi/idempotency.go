package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

const (
	// IdempotencyKeyHeader carries the client's per-submission key on money POSTs.
	IdempotencyKeyHeader = "Idempotency-Key"
	// IdempotencyStatusHeader marks a response this layer produced instead of the handler:
	// a stored response served again, or the 409 for a duplicate of a running request. The
	// app uses the latter to tell "still running, keep the key" from a business 409.
	IdempotencyStatusHeader     = "Idempotency-Status"
	idempotencyStatusReplayed   = "replayed"
	idempotencyStatusInProgress = "in_progress"

	idempotencyKeyMinLength = 8
	idempotencyKeyMaxLength = 128

	// idempotencyKeyTTL is how long a key replays its stored response.
	idempotencyKeyTTL = 24 * time.Hour
	// idempotencyAbandonedAfter is when an in_progress claim stops blocking retries. No
	// handler outlives the server write timeout (3m), so an older claim belongs to a
	// process that died mid-request and would otherwise answer 409 until the key expires.
	idempotencyAbandonedAfter = 5 * time.Minute
	// idempotencyFinishTimeout bounds the store write after the handler returns. It runs
	// detached from the request context: a client that hung up must not lose the record.
	idempotencyFinishTimeout = 5 * time.Second
)

// idempotentPathSuffixes are the user-initiated POST routes that move or commit USDC.
// Agent intents authenticate with an agent key, not a user, so this middleware does not
// cover them: they dedupe on the idempotencyKey field in their own body (AgentIntentService).
var idempotentPathSuffixes = []string{
	"/fund",
	"/withdraw-to-balance",
	"/withdrawals",
	"/retry",
	"/proposals",
	"/leave",
}

// IdempotencyStore persists claimed keys and their responses.
type IdempotencyStore interface {
	GetUserByPrivyUserID(ctx context.Context, privyUserID string) (postgres.User, bool, error)
	ClaimIdempotencyKey(ctx context.Context, params postgres.ClaimIdempotencyKeyParams) (postgres.IdempotencyKeyRow, bool, error)
	CompleteIdempotencyKey(ctx context.Context, userID, key string, claimedAt time.Time, status int, contentType string, body []byte) error
	ReleaseIdempotencyKey(ctx context.Context, userID, key string, claimedAt time.Time) error
}

// SessionVerifier authenticates a bearer token. privy.Client satisfies it.
type SessionVerifier interface {
	VerifySession(ctx context.Context, token privy.AccessToken) (privy.Identity, error)
}

// Idempotency makes money POSTs safe to retry. A request carrying Idempotency-Key runs at
// most once per (user, key): a retry with the same body replays the stored response, a
// different body is refused with 422, and a duplicate that arrives while the first is
// still running gets 409. The header is optional; without it requests behave as before.
type Idempotency struct {
	store    IdempotencyStore
	sessions SessionVerifier
	now      func() time.Time
	// routePattern resolves the mux pattern for a request. Optional; see WithRoutePattern.
	routePattern func(*http.Request) string
}

// NewIdempotency wires the idempotency layer.
func NewIdempotency(store IdempotencyStore, sessions SessionVerifier) *Idempotency {
	return &Idempotency{store: store, sessions: sessions, now: time.Now}
}

// WithClock replaces the time source. Intended for tests.
func (i *Idempotency) WithClock(now func() time.Time) *Idempotency {
	i.now = now
	return i
}

// WithRoutePattern lets answers produced here (replays, 409, 422) be labelled with their mux
// route pattern. Metrics reads r.Pattern, which only the mux sets, and those answers never
// reach the mux.
func (i *Idempotency) WithRoutePattern(resolve func(*http.Request) string) *Idempotency {
	i.routePattern = resolve
	return i
}

// Middleware applies the idempotency contract to the money POST routes.
//
// Keys are scoped to the authenticated user, so the token is verified here before any
// lookup. A request that fails authentication is passed through untouched and the handler
// answers 401 as it always has; nothing is stored for it, which lets the client refresh
// its token and retry under the same key.
func (i *Idempotency) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || !isIdempotentPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			rawKey, present := r.Header[http.CanonicalHeaderKey(IdempotencyKeyHeader)]
			if !present {
				next.ServeHTTP(w, r)
				return
			}
			if i.routePattern != nil && r.Pattern == "" {
				r.Pattern = i.routePattern(r)
			}
			ctx := r.Context()
			key := strings.TrimSpace(strings.Join(rawKey, ","))
			if !validIdempotencyKey(key) {
				writeJSONError(ctx, w, http.StatusBadRequest, "invalid Idempotency-Key: use 8-128 characters from A-Z a-z 0-9 . _ : -")
				return
			}

			userID, authenticated, err := i.authenticate(ctx, r)
			if err != nil {
				slog.ErrorContext(ctx, "idempotency user lookup failed", "path", r.URL.Path, "err", err.Error())
				writeJSONError(ctx, w, http.StatusInternalServerError, "internal server error")
				return
			}
			if !authenticated {
				next.ServeHTTP(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				if isBodyTooLarge(err) {
					writeJSONError(ctx, w, http.StatusRequestEntityTooLarge, bodyTooLargeMessage)
					return
				}
				writeJSONError(ctx, w, http.StatusBadRequest, "invalid request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			now := i.now().UTC()
			route := r.Method + " " + strings.TrimSuffix(r.URL.Path, "/")
			requestHash := hashIdempotentRequest(body)
			row, claimed, err := i.store.ClaimIdempotencyKey(ctx, postgres.ClaimIdempotencyKeyParams{
				UserID:          userID,
				Key:             key,
				Route:           route,
				RequestHash:     requestHash,
				Now:             now,
				ExpiredBefore:   now.Add(-idempotencyKeyTTL),
				AbandonedBefore: now.Add(-idempotencyAbandonedAfter),
			})
			if err != nil {
				// Fail closed: running the handler without the claim is exactly the
				// duplicate this layer exists to prevent.
				slog.ErrorContext(ctx, "idempotency claim failed", "path", r.URL.Path, "err", err.Error())
				writeJSONError(ctx, w, http.StatusInternalServerError, "internal server error")
				return
			}

			if !claimed {
				i.answerExisting(ctx, w, r, row, route, requestHash)
				return
			}
			i.run(w, r, next, row)
		})
	}
}

// authenticate resolves the caller's user id. ok is false when the request carries no
// valid session or the user does not exist yet; the handler reports those itself.
func (i *Idempotency) authenticate(ctx context.Context, r *http.Request) (userID string, ok bool, err error) {
	token, present := bearerToken(r)
	if !present {
		return "", false, nil
	}
	identity, err := i.sessions.VerifySession(ctx, privy.AccessToken(token))
	if err != nil {
		return "", false, nil
	}
	user, found, err := i.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, nil
	}
	return user.ID, true, nil
}

func (i *Idempotency) answerExisting(ctx context.Context, w http.ResponseWriter, r *http.Request, row postgres.IdempotencyKeyRow, route, requestHash string) {
	if row.Route != route || row.RequestHash != requestHash {
		slog.WarnContext(ctx, "idempotency key reused for a different request", "path", r.URL.Path, "stored_route", row.Route)
		writeJSONError(ctx, w, http.StatusUnprocessableEntity, "Idempotency-Key was already used for a different request")
		return
	}
	if row.State != postgres.IdempotencyStateCompleted {
		slog.InfoContext(ctx, "idempotency duplicate while in progress", "path", r.URL.Path)
		w.Header().Set(IdempotencyStatusHeader, idempotencyStatusInProgress)
		writeJSONError(ctx, w, http.StatusConflict, "a request with this Idempotency-Key is still in progress")
		return
	}

	slog.InfoContext(ctx, "idempotency replay", "path", r.URL.Path, "status", row.ResponseStatus)
	if row.ResponseContentType != "" {
		w.Header().Set("Content-Type", row.ResponseContentType)
	}
	w.Header().Set(IdempotencyStatusHeader, idempotencyStatusReplayed)
	w.WriteHeader(row.ResponseStatus)
	_, _ = w.Write(row.ResponseBody)
}

// run executes the handler under a claimed key and records the outcome. 5xx responses and
// panics release the key instead: they describe a server fault, not the request's result,
// so the client's retry must reach the handler again.
func (i *Idempotency) run(w http.ResponseWriter, r *http.Request, next http.Handler, claim postgres.IdempotencyKeyRow) {
	capture := &idempotencyCapture{ResponseWriter: w, status: http.StatusOK}
	finished := false
	defer func() {
		if finished {
			return
		}
		i.release(r, claim)
	}()

	next.ServeHTTP(capture, r)

	if capture.status >= http.StatusInternalServerError {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), idempotencyFinishTimeout)
	defer cancel()
	err := i.store.CompleteIdempotencyKey(ctx, claim.UserID, claim.Key, claim.CreatedAt, capture.status, capture.Header().Get("Content-Type"), capture.body.Bytes())
	if err != nil {
		// The response is already on the wire. Leaving the claim in_progress keeps a
		// retry from running the money path twice; it answers 409 until the claim ages out.
		slog.ErrorContext(ctx, "idempotency complete failed", "path", r.URL.Path, "status", capture.status, "err", err.Error())
	}
	finished = true
}

func (i *Idempotency) release(r *http.Request, claim postgres.IdempotencyKeyRow) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), idempotencyFinishTimeout)
	defer cancel()
	if err := i.store.ReleaseIdempotencyKey(ctx, claim.UserID, claim.Key, claim.CreatedAt); err != nil {
		slog.ErrorContext(ctx, "idempotency release failed", "path", r.URL.Path, "err", err.Error())
	}
}

// idempotencyCapture copies the handler's response so it can be stored for replay.
type idempotencyCapture struct {
	http.ResponseWriter
	status int
	wrote  bool
	body   bytes.Buffer
}

func (c *idempotencyCapture) WriteHeader(status int) {
	if c.wrote {
		return
	}
	c.wrote = true
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *idempotencyCapture) Write(b []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	c.body.Write(b)
	return c.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (c *idempotencyCapture) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

func isIdempotentPath(path string) bool {
	path = strings.TrimSuffix(path, "/")
	for _, suffix := range idempotentPathSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func validIdempotencyKey(key string) bool {
	if len(key) < idempotencyKeyMinLength || len(key) > idempotencyKeyMaxLength {
		return false
	}
	for _, c := range key {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == ':':
		default:
			return false
		}
	}
	return true
}

// hashIdempotentRequest fingerprints the request body. JSON is hashed in canonical form
// (sorted keys, no insignificant whitespace) because a client re-encoding the same
// payload for a retry is free to order its keys differently.
func hashIdempotentRequest(body []byte) string {
	canonical := body
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err == nil && !decoder.More() {
			if encoded, err := json.Marshal(value); err == nil {
				canonical = encoded
			}
		}
	} else {
		canonical = nil
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}
