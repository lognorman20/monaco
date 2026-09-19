# Debugging "I can't log in"

The iOS app now shows *why* sign-in failed (see the session gate's error screen
and, in DEBUG builds, a monospaced detail line with the status/URLError code
and the API base URL). Use that message first — it usually points straight at
one of the items below.

## Checklist

1. **Is the backend actually running, on `127.0.0.1:8080`, and restarted since you last pulled?**
   - `just run backend` (or `just run` for backend + mobile together).
   - A stale backend process from before a pull won't have new migrations —
     restart it after every pull that touches `apps/backend` or `supabase/migrations`.
   - Sanity check: `curl -s http://127.0.0.1:8080/health` should return `200`.

2. **Did migrations 014–016 apply?**
   - `supabase/migrations/000014_proposal_comments.sql`,
     `000015_group_messages.sql`, `000016_faker_flags.sql`.
   - `just run backend` applies pending migrations on boot
     (`scripts/apply-migrations.sh`). If the backend was already running when
     you pulled, it won't have picked these up — stop and restart it.
   - If boot fails outright, check `.logs/<timestamp>/backend.log` for
     `boot failed` — that's almost always a migration or `DATABASE_URL` problem.

3. **Does `.env.keys` decrypt `.env.local`?**
   - `.env.local` is committed encrypted (dotenvx); `.env.keys` is the
     gitignored private key file that decrypts it. Without it, Privy
     credentials and `DATABASE_URL` silently resolve to nothing.
   - Quick check: `dotenvx get PRIVY_APP_ID -f .env.local` should print a value,
     not an error.

4. **Did `scripts/ensure-ios-privy-config.sh` regenerate the iOS Privy config after you pulled?**
   - It writes `apps/mobile/Config/Privy.local.xcconfig` and
     `Privy.local.Info.plist` from `.env.local`. Both are gitignored, so a
     fresh pull (or a change to `PRIVY_APP_ID`/`PRIVY_APP_CLIENT_ID` in
     `.env.local`) leaves them stale until you re-run it:
     ```
     dotenvx run -q -f .env.local -- bash scripts/ensure-ios-privy-config.sh
     ```
   - `just run mobile` runs this for you before launching the simulator — if
     you build straight from Xcode instead, run it manually first.

5. **Do the app's Privy credentials match the backend's?**
   - `PRIVY_APP_ID` / `PRIVY_APP_CLIENT_ID` in `.env.local` (what the app
     builds with) must point at the same Privy app as the backend's
     `PRIVY_APP_ID` / `PRIVY_APP_SECRET` / verification key.
   - A mismatch here is the classic "can't log in" cause: the app authenticates
     against Privy fine, but `POST /v1/auth/session` then 401s because the
     backend can't verify a token minted for a different Privy app. This now
     surfaces on the login screen as "We couldn't verify your sign-in. Try
     again." instead of silently bouncing you back — if you see that message,
     start here.

6. **Where the logs are.**
   - Backend: `.logs/<timestamp>/backend.log` (one directory per `just run` /
     `just run backend` invocation — use the most recent).
   - Mobile: `.logs/<timestamp>/mobile.log`, plus the app's own `os.Logger`
     output (category `session`) in Console.app or `log stream --predicate
     'subsystem == "<bundle id>"'`.
   - Useful greps against `backend.log`:
     - `grep 'POST /v1/auth/session' .logs/<timestamp>/backend.log` — confirms
       the app is even reaching the backend, and shows the response status.
     - `grep 'boot failed' .logs/<timestamp>/backend.log` — startup/migration
       problems.
     - `grep 'invalid_token' .logs/<timestamp>/backend.log` — the backend
       rejected the access token (expired, wrong Privy app, or clock skew).

7. **Xcode confused after a launch-screen or storyboard change?**
   - `just reset mobile` does an `xcodebuild clean` targeting the resolved
     simulator. Reach for it if the build looks stale or a UI change (e.g. the
     launch screen) doesn't take effect.

## Reading the on-screen error

The session gate maps failures onto three user-facing messages (see
`SessionErrorMapping` in `apps/mobile/Monaco/Features/Shell/SessionErrorMapping.swift`):

| Symptom | Message shown | Likely cause |
| --- | --- | --- |
| Can't connect at all (`URLError`) | "Can't reach Monaco. Is the server running?" | Backend not running / wrong host / wrong port. See checklist item 1. |
| Backend returned 5xx | "Monaco's server hit a problem. Try again in a moment." | Backend crash, unhandled panic, or a downstream dependency (DB, RPC) is down. Check `backend.log`. |
| Backend returned 401 opening the session | (shown on the **login** screen, after signing out) "We couldn't verify your sign-in. Try again." | Privy app-id / verification-key mismatch between the app build and the backend env. See checklist items 4–5. |

In DEBUG builds only, a small monospaced line under the message on the session
gate adds the exact status code / `URLError` code and the API base URL the app
is hitting, so you don't have to guess.
