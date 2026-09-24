# trymonaco.xyz

Waitlist landing page. Static HTML plus two Vercel serverless functions. No build step.

| Path | What it is |
|---|---|
| `index.html` | The page |
| `api/waitlist.js` | `POST` signup to the Supabase `waitlist` table |
| `api/health.js` | `GET` status of Supabase and config |
| `lib/waitlist.js` | Signup logic, tested in `test/` |
| `assets/demo.mp4` | Not wired into the page yet. Drop the launch video here and add the section back when it's ready |

## Run and test

```bash
cd apps/web && npm test
```

```bash
cd apps/web && npx vercel dev
```

## Deploy

1. Import the repo in Vercel with root directory `apps/web`, framework "Other".
2. Set the env vars below for Production and Preview.
3. Add `trymonaco.xyz` and `www.trymonaco.xyz` under Domains and point DNS at Vercel.
4. Check that `https://trymonaco.xyz/api/health` returns `"status": "ok"`.

| Env var | Value |
|---|---|
| `SUPABASE_URL` | `https://<ref>.supabase.co` for the Monaco project |
| `SUPABASE_ANON_KEY` | The project's anon (publishable) key. Never the service role key |
| `IP_HASH_SALT` | Any long random string. IPs are stored only as salted hashes |
| `ALLOWED_ORIGINS` | Optional. Defaults to monacolabs.xyz and trymonaco.xyz (with and without `www`). Set to the preview URL on Preview |

Signups are in the `waitlist` table. Export with the Supabase table editor or `select email, source, created_at from waitlist order by created_at`.
