package privy

import (
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/logsnippet"
)

func logAPIStart(method, path string, idempotent bool) {
	args := []any{"method", method, "path", path}
	if idempotent {
		args = append(args, "idempotent", true)
	}
	slog.Info("privy api request", args...)
}

func logAPIResult(method, path string, status int, respBody []byte, err error) {
	if err != nil {
		args := []any{"method", method, "path", path, "status", status, "err", err}
		if snippet := logsnippet.Body(respBody); snippet != "" {
			args = append(args, "response_body", snippet)
		}
		slog.Warn("privy api request failed", args...)
		return
	}
	if status < 200 || status >= 300 {
		args := []any{"method", method, "path", path, "status", status}
		if snippet := logsnippet.Body(respBody); snippet != "" {
			args = append(args, "response_body", snippet)
		}
		slog.Warn("privy api response error", args...)
		return
	}
	slog.Info("privy api response",
		"method", method,
		"path", path,
		"status", status,
	)
}

func logSolanaRPC(method string, status int, respBody []byte, err error) {
	if err != nil {
		args := []any{"method", method, "err", err}
		if status > 0 {
			args = append(args, "status", status)
		}
		if snippet := logsnippet.Body(respBody); snippet != "" {
			args = append(args, "response_body", snippet)
		}
		slog.Warn("privy solana rpc failed", args...)
		return
	}
	slog.Info("privy solana rpc", "method", method, "ok", true)
}

func logSweepFailed(stage, memberAddress, treasuryAddress string, amount int64, err error) {
	slog.Error("privy sweep failed",
		"stage", stage,
		"member_address", memberAddress,
		"treasury_address", treasuryAddress,
		"amount", amount,
		"err", err,
	)
}

func logVerifySession(ok bool, privyUserID string) {
	if ok {
		slog.Info("privy verify session", "ok", true, "privy_user_id", privyUserID)
		return
	}
	slog.Info("privy verify session", "ok", false)
}

func logFake(op string, args ...any) {
	all := append([]any{"op", op}, args...)
	slog.Debug("privy fake", all...)
}
