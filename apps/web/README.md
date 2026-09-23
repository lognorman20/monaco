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

The waitlist lives in the main Monaco Supabase project. The landing page holds only the anon key, which can call `join_waitlist()` and nothing else. It never gets the service role key. The function also checks a writer secret and rate-limits to 5 signups per network per hour.

1. Apply `supabase/migrations/000019_waitlist.sql` to the Monaco project with the backend's usual migrate step.
2. Generate a writer secret with `openssl rand -hex 32`. Store its hash in the Supabase SQL editor with the `INSERT INTO waitlist_settings` line from the top of the migration.
3. Import the repo in Vercel with root directory `apps/web`, framework "Other".
4. Set the env vars below for Production and Preview.
5. Add `trymonaco.xyz` and `www.trymonaco.xyz` under Domains and point DNS at Vercel.
6. Check that `https://trymonaco.xyz/api/health` returns `"status": "ok"`.

| Env var | Value |
|---|---|
| `SUPABASE_URL` | `https://<ref>.supabase.co` for the Monaco project |
| `SUPABASE_ANON_KEY` | The project's anon (publishable) key. Never the service role key |
| `WAITLIST_WRITER_SECRET` | The secret from step 2, in plain text |
| `IP_HASH_SALT` | Any long random string. IPs are stored only as salted hashes |
| `ALLOWED_ORIGINS` | Optional. Defaults to the two trymonaco.xyz origins. Set to the preview URL on Preview |

To rotate the writer secret, run the step 2 insert again with a new value, then update the Vercel env var.

Signups are in the `waitlist` table. Export with the Supabase table editor or `select email, source, created_at from waitlist order by created_at`.
