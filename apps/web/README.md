# trymonaco.xyz

Waitlist landing page. Static HTML plus two Vercel serverless functions. No build step.

| Path | What it is |
|---|---|
| `index.html` | The page |
| `api/waitlist.js` | `POST` signup: validates, then calls the `join_waitlist()` database function |
| `api/health.js` | `GET` status of Supabase and config |
| `lib/waitlist.js` | Signup logic, tested in `test/` |
| `assets/demo.mp4` | Drop the launch video here. The page shows a placeholder until it exists |

## Run and test

```bash
cd apps/web && npm test
```

```bash
cd apps/web && npx vercel dev
```

## Deploy

The waitlist lives in the main Monaco Supabase project. Its schema is one migration, `supabase/migrations/000019_waitlist.sql`, applied like every other migration: at backend boot, or with `go run ./cmd/migrate` from `apps/backend`. Nothing is run by hand in the SQL editor.

The landing page holds only the anon key, which can call `join_waitlist()` and nothing else. It never gets the service role key. The database function enforces the limits: 5 signups per network per hour, and 60 per minute across everyone.

1. Merge the PR. The migration applies on the next backend deploy or `cmd/migrate` run.
2. Import the repo in Vercel with root directory `apps/web`, framework "Other".
3. Set the env vars below for Production and Preview.
4. Add `trymonaco.xyz` and `www.trymonaco.xyz` under Domains and point DNS at Vercel.
5. Check that `https://trymonaco.xyz/api/health` returns `"status": "ok"`. It reports `waitlist table missing` until the migration has run.

| Env var | Value |
|---|---|
| `SUPABASE_URL` | `https://<ref>.supabase.co` for the Monaco project |
| `SUPABASE_ANON_KEY` | The project's anon (publishable) key. Never the service role key |
| `IP_HASH_SALT` | Any long random string. IPs are stored only as salted hashes |
| `ALLOWED_ORIGINS` | Optional. Defaults to the two trymonaco.xyz origins. Set to the preview URL on Preview |

Signups are in the `waitlist` table. Export with the Supabase table editor or `select email, source, created_at from waitlist order by created_at`.
