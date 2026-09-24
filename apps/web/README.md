# trymonaco.xyz

Waitlist landing page. Static HTML plus two serverless functions. No build step.

| Path | What it is |
|---|---|
| `index.html` | The page |
| `lib/waitlist.js` | Signup and health logic, tested in `test/` |
| `functions/api/waitlist.js`, `functions/api/health.js` | Cloudflare Pages adapters |
| `api/waitlist.js`, `api/health.js` | Vercel adapters |
| `_headers`, `wrangler.toml` | Cloudflare config |
| `vercel.json` | Vercel config |
| `assets/video-poster.png` | Poster for the demo video slot |

`api/` and `functions/` are two thin adapters over the same two functions in
`lib/waitlist.js` — `handleSignup()` and `checkHealth()`, both returning
`{ status, body }`. Both hosts are live on purpose so the domain can move without
downtime. Once `trymonaco.xyz` points at Cloudflare, delete `api/`, `vercel.json`
and the `dev` script.

## Run and test

```bash
cd apps/web && npm test                        # both adapters and the shared logic
cd apps/web && npx wrangler pages dev           # Cloudflare, serves /api/* for real
cd apps/web && npx vercel dev                   # Vercel
```

`wrangler pages dev` reads no secrets on its own; pass them for a local run:

```bash
npx wrangler pages dev --binding SUPABASE_URL=… SUPABASE_ANON_KEY=… IP_HASH_SALT=local ALLOWED_ORIGINS=http://127.0.0.1:8788
```

## Deploy to Cloudflare Pages

Apply the waitlist migrations (`supabase/migrations/000019`–`000022`) to the hosted
database first. Without them `join_waitlist()` does not exist, `/api/health` reports
`supabase: "waitlist table missing"`, and signups return a 502 rather than storing
anything.

| Setting | Value |
|---|---|
| Project source | Connect to Git, repo `lognorman20/monaco`, production branch `main` |
| Root directory | `apps/web` |
| Framework preset | None |
| Build command | *(empty — there is no build step)* |
| Build output directory | `.` |

`wrangler.toml` supplies the `nodejs_compat` compatibility flag, which the
`node:crypto` import in `lib/waitlist.js` needs. It is checked in so the deploy
does not depend on dashboard state.

| Env var | Value |
|---|---|
| `SUPABASE_URL` | `https://<ref>.supabase.co` for the Monaco project |
| `SUPABASE_ANON_KEY` | The project's anon (publishable) key. Never the service role key |
| `IP_HASH_SALT` | Any long random string. IPs are stored only as salted hashes |
| `ALLOWED_ORIGINS` | Optional. Defaults to monacolabs.xyz and trymonaco.xyz (with and without `www`). Set it to the preview URL on Preview |

Set all four on both Production and Preview, then:

1. Workers & Pages → the project → Custom domains → add `trymonaco.xyz` and
   `www.trymonaco.xyz`. Cloudflare writes the DNS records itself if the zone is
   already on the account; otherwise move the nameservers first.
2. Check `https://trymonaco.xyz/api/health` returns `"status": "ok"`.
3. Sign up once and confirm the row lands in `waitlist`.

Notes:

- `_headers` replaces `vercel.json`'s headers. Keep the two in sync while both hosts
  are live.
- `cleanUrls` needs no config: Pages serves `/foo` for `foo.html` and redirects
  `/foo.html` to `/foo` by default.
- Everything in `apps/web` is uploaded as a static asset, including `lib/` and
  `test/`. That is the price of having no build step; the same files are already
  public on GitHub.

## Demo video

The video is never committed and never served from Pages. Host it on **R2**, where
egress is free, or as an **unlisted YouTube/Vimeo embed**.

To wire it up, set `VIDEO_URL` in the script at the bottom of `index.html` — that one
line is the whole edit. Empty means the "coming soon" placeholder stays.

| `VIDEO_URL` | What renders |
|---|---|
| `""` | The placeholder |
| `https://media.trymonaco.xyz/demo.mp4` | `<video controls preload="none" poster>` |
| `https://www.youtube.com/embed/<id>` | The poster behind a play button; the iframe is built on click |

`preload="none"` is the point: browsers otherwise fetch the first chunk of a video on
every page load, watched or not, which is how free bandwidth turns into a bill. The
embed path is the same idea for a third party — nothing is requested until someone
presses play. Nothing autoplays on load, and the caption under the frame appears with
the video.

For R2: create a bucket, upload `demo.mp4`, connect a custom domain such as
`media.trymonaco.xyz` (public bucket access, no signed URLs needed for a launch video),
and use that URL. For captions, drop a WebVTT file at `assets/demo.vtt` and uncomment
the four `cc` lines in the player below `VIDEO_URL`.

## Data

Signups are in the `waitlist` table. Export with the Supabase table editor or
`select email, twitter, source, created_at from waitlist order by created_at`.
