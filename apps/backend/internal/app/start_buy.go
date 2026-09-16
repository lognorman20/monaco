package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// ErrQuoteNotRoutable means Jupiter returned no route for the requested buy.
var ErrQuoteNotRoutable = errors.New("quote not routable")

// ErrDevRouteBlocked means the temporary dev-only buy route is disabled.
var ErrDevRouteBlocked = errors.New("dev route blocked")

// StartBuyRequest is input for the M3 dev stub buy gate (M4 vote gate later).
type StartBuyRequest struct {
	GroupID    string
	UserID     string
	Symbol     string
	USDCAmount int64
}

// StartBuyResult holds a routable Jupiter quote ready for execute.
type StartBuyResult struct {
	OutputMint string
	Quote      jupiter.BuyQuote
}

// BuyService gates treasury buys behind quote availability.
type BuyService struct {
	jupiter  jupiter.Client
	xstocks  xstocks.Resolver
}

// NewBuyService wires Jupiter quote and xStocks mint resolution.
func NewBuyService(jupiterClient jupiter.Client, resolver xstocks.Resolver) *BuyService {
	return &BuyService{
		jupiter: jupiterClient,
		xstocks: resolver,
	}
}

// StartBuy resolves the xStock mint and refuses when Jupiter has no route.
func (s *BuyService) StartBuy(ctx context.Context, req StartBuyRequest) (StartBuyResult, error) {
	logSwapQuoteAttempt(req.GroupID, req.UserID, req.Symbol, req.USDCAmount)

	outputMint, err := s.xstocks.ResolveSolanaMint(ctx, req.Symbol)
	if err != nil {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, err.Error())
		return StartBuyResult{}, err
	}

	quote, err := s.jupiter.QuoteBuy(ctx, jupiter.QuoteBuyParams{
		GroupID:    req.GroupID,
		UserID:     req.UserID,
		Symbol:     req.Symbol,
		OutputMint: outputMint,
		USDCAmount: req.USDCAmount,
	})
	if err != nil {
		reason := err.Error()
		if errors.Is(err, jupiter.ErrNoRoute) {
			reason = "no route"
		}
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, reason)
		if errors.Is(err, jupiter.ErrNoRoute) {
			return StartBuyResult{}, fmt.Errorf("%w: %s", ErrQuoteNotRoutable, reason)
		}
		return StartBuyResult{}, err
	}
	if !quote.Routable {
		logSwapRefusal(req.GroupID, req.UserID, req.Symbol, "no route")
		return StartBuyResult{}, fmt.Errorf("%w: no route", ErrQuoteNotRoutable)
	}

	return StartBuyResult{
		OutputMint: outputMint,
		Quote:      quote,
	}, nil
}

// DevBuyEnabled reports whether POST /v1/dev/groups/{id}/buy is available.
func DevBuyEnabled() bool {
	value := strings.TrimSpace(os.Getenv("DEV_BUY_ENABLED"))
	return value == "1" || strings.EqualFold(value, "true")
}

// AllowDevRoute returns nil when StartBuy may run via the M3 dev stub route.
func (s *BuyService) AllowDevRoute() error {
	if !DevBuyEnabled() {
		return ErrDevRouteBlocked
	}
	return nil
}

// StartBuyViaDevRoute runs StartBuy only when the dev stub route is enabled.
func (s *BuyService) StartBuyViaDevRoute(ctx context.Context, req StartBuyRequest) (StartBuyResult, error) {
	if err := s.AllowDevRoute(); err != nil {
		return StartBuyResult{}, err
	}
	return s.StartBuy(ctx, req)
}

// DevBuyService orchestrates the temporary M3 dev-only execute-buy path.
type DevBuyService struct {
	swap  *SwapService
	store *postgres.Store
	privy privy.Client
}

// NewDevBuyService wires the dev stub execute-buy dependencies.
func NewDevBuyService(swap *SwapService, store *postgres.Store, privyClient privy.Client) *DevBuyService {
	return &DevBuyService{
		swap:  swap,
		store: store,
		privy: privyClient,
	}
}

// DevBuyRequest is input for the dev stub execute-buy route.
type DevBuyRequest struct {
	GroupID string
	Symbol  string
	USDC    int64
}

// DevBuyResult is the persisted buy from the dev stub route.
type DevBuyResult struct {
	TransactionID string
	GroupID       string
	Symbol        string
	Status        string
	TxSignature   string
	Created       bool
}

// ExecuteDevBuy authenticates a member and runs DevExecuteBuy without a vote gate.
func (s *DevBuyService) ExecuteDevBuy(ctx context.Context, accessToken string, req DevBuyRequest) (DevBuyResult, error) {
	if !DevBuyEnabled() {
		return DevBuyResult{}, ErrDevRouteBlocked
	}
	if req.GroupID == "" {
		return DevBuyResult{}, ErrGroupNotFound
	}
	if strings.TrimSpace(req.Symbol) == "" {
		return DevBuyResult{}, fmt.Errorf("symbol is required")
	}
	if req.USDC <= 0 {
		return DevBuyResult{}, fmt.Errorf("usdc must be positive")
	}

	identity, err := s.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return DevBuyResult{}, privy.ErrInvalidToken
		}
		return DevBuyResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := s.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return DevBuyResult{}, err
	}
	if !found {
		return DevBuyResult{}, ErrUserNotFound
	}

	group, found, err := s.store.GetGroupByID(ctx, req.GroupID)
	if err != nil {
		return DevBuyResult{}, err
	}
	if !found {
		return DevBuyResult{}, ErrGroupNotFound
	}
	if group.CreatorUserID != user.ID {
		return DevBuyResult{}, ErrNotGroupMember
	}

	executeResult, err := s.swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
		GroupID:    req.GroupID,
		UserID:     user.ID,
		Symbol:     req.Symbol,
		USDCAmount: req.USDC,
	})
	if err != nil {
		return DevBuyResult{}, err
	}

	txSignature := ""
	if executeResult.Transaction.TxSignature.Valid {
		txSignature = executeResult.Transaction.TxSignature.String
	}

	return DevBuyResult{
		TransactionID: executeResult.Transaction.ID,
		GroupID:       executeResult.Transaction.GroupID,
		Symbol:        req.Symbol,
		Status:        executeResult.Transaction.Status,
		TxSignature:   txSignature,
		Created:       executeResult.Created,
	}, nil
}
