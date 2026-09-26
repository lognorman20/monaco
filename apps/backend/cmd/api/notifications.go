package main

import (
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/apns"
	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// pushSender is Apple push when APNS_* is set and valid, and a logger otherwise: the inbox
// works either way, and a bad key never stops the API from booting.
func pushSender(cfg config.APNSConfig, store *postgres.Store) app.PushSender {
	if !cfg.Enabled() {
		if cfg.Partial() {
			slog.Error("apple push off: set all of APNS_KEY_ID, APNS_TEAM_ID and APNS_PRIVATE_KEY")
		} else {
			slog.Info("apple push off", "reason", "APNS_KEY_ID, APNS_TEAM_ID or APNS_PRIVATE_KEY unset; pushes are logged")
		}
		return app.LogPushSender{}
	}
	env, err := apns.ParseEnv(cfg.Env)
	if err != nil {
		slog.Error("apple push off: bad APNS_ENV", "err", err)
		return app.LogPushSender{}
	}
	client, err := apns.NewClient(apns.Config{
		KeyID:         cfg.KeyID,
		TeamID:        cfg.TeamID,
		PrivateKeyPEM: cfg.PrivateKey,
		BundleID:      cfg.BundleID,
		Env:           env,
	})
	if err != nil {
		slog.Error("apple push off: bad APNs credentials", "err", err)
		return app.LogPushSender{}
	}
	slog.Info("apple push ready", "env", string(env), "bundle_id", cfg.BundleID, "key_id", cfg.KeyID)
	return app.NewAPNsPushSender(client, store)
}
