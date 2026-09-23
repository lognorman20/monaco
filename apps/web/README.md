# trymonaco.xyz

Waitlist landing page. Static HTML plus two Vercel serverless functions. No build step.

| Path | What it is |
|---|---|
| `index.html` | The page |
| `api/waitlist.js` | `POST` signup: validates, rate-limits (5 per IP per hour), writes to Supabase |
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

1. Apply `supabase/migrations/000019_waitlist.sql` to the hosted Supabase project.
2. Import the repo in Vercel with root directory `apps/web`, framework "Other".
3. Set env vars in Vercel for Production and Preview.
4. Add `trymonaco.xyz` and `www.trymonaco.xyz` under Domains and point DNS at Vercel.
5. Check `https://trymonaco.xyz/api/health` returns `"status": "ok"`.

| Env var | Value |
|---|---|
| `SUPABASE_URL` | `https://<ref>.supabase.co` |
| `SUPABASE_SERVICE_ROLE_KEY` | Service role key. Server only, never in the page |
| `IP_HASH_SALT` | Any long random string. IPs are stored only as salted hashes |
| `ALLOWED_ORIGINS` | Optional. Defaults to the two trymonaco.xyz origins. Set to the preview URL on Preview |

Signups are in the `waitlist` table. Export with the Supabase table editor or `select email, source, created_at from waitlist order by created_at`.
