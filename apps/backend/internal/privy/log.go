package privy

import "log/slog"

func logAPIStart(method, path string, idempotent bool) {
	args := []any{"method", method, "path", path}
	if idempotent {
		args = append(args, "idempotent", true)
	}
	slog.Info("privy api request", args...)
}

func logAPIResult(method, path string, status int, err error) {
	if err != nil {
		slog.Warn("privy api request failed",
			"method", method,
			"path", path,
			"status", status,
			"err", err,
		)
		return
	}
	level := slog.LevelInfo
	if status < 200 || status >= 300 {
		level = slog.LevelWarn
	}
	slog.Log(nil, level, "privy api response",
		"method", method,
		"path", path,
		"status", status,
	)
}

func logSolanaRPC(method string, err error) {
	if err != nil {
		slog.Warn("privy solana rpc failed", "method", method, "err", err)
		return
	}
	slog.Info("privy solana rpc", "method", method, "ok", true)
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
