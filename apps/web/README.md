# trymonaco.xyz

Waitlist landing page and the cabal invite page. Static HTML plus serverless functions. No build step.

| Path | What it is |
|---|---|
| `index.html` | The waitlist page |
| `join/index.html` | The invite page, served at `/join/<code>` |
| `lib/waitlist.js` | Signup and health logic, tested in `test/` |
| `lib/invite.js` | Invite page logic (code parsing, preview fetch, page copy), loaded by the page as a module and tested in `test/` |
| `config.js` | `window.MONACO_API_BASE` for the invite preview |
| `functions/api/waitlist.js`, `functions/api/health.js` | Cloudflare Pages adapters |
| `functions/join/[code].js` | Cloudflare: serves `join/index.html` for every `/join/<code>` |
| `api/waitlist.js`, `api/health.js` | Vercel adapters |
| `.well-known/apple-app-site-association` | Universal links: `/join/*` opens the app |
| `_headers`, `wrangler.toml` | Cloudflare config |
| `vercel.json` | Vercel config (`/join/:code` rewrite, AASA content type) |
| `assets/video-poster.png` | Poster for the demo video slot |

## Invite links

`https://trymonaco.xyz/join/<code>` is what a member shares from the app. With Monaco
installed, iOS opens the app on the join screen with the code filled in (universal link, via
`.well-known/apple-app-site-association`). Without it, the page shows the cabal's mark, name,
member count and pot, an "Open in Monaco" button (`monaco://join/<code>`), the code with a copy
button, and the App Store link (a placeholder until the listing exists).

The page reads `MONACO_API_BASE + "/v1/invites/<code>"` in the browser. Until the API has a
public URL, `config.js` leaves it empty and the page shows the code and buttons without the
cabal's name. To turn the preview on:

1. Set `window.MONACO_API_BASE` in `config.js` to the API's public https URL.
2. Add `https://trymonaco.xyz` (and the `www` host) to the API's `CORS_ALLOWED_ORIGINS`.

The association file names team `JSF53DFS29` (the project's `DEVELOPMENT_TEAM`) and bundle
`com.monaco.app`. Apple fetches it from its CDN when the app is installed, so a change takes a
while to reach phones. The app's Associated Domains capability must be on for that bundle id in
the developer portal.

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

## Demo video (not on the page yet)

There is deliberately no video section on the page — it goes back when there is a video
to put in it. What matters is that it stays cheap when it does.

**Never commit the video, and never serve it from Pages.** Host it on **Cloudflare R2**,
where egress is free, or as an **unlisted YouTube/Vimeo embed**. A 20MB file served from
a metered host is roughly 100GB after five thousand plays, which is where a free tier
stops being free.

To add it back:

1. A `<section class="video">` with a 16/9 frame, after the hero and before `</main>`.
2. For a file: `<video controls preload="none" poster="/assets/video-poster.png">`.
   `preload="none"` is the whole point — browsers otherwise fetch the first chunk of a
   video on every page load, watched or not.
3. For an embed: show the poster behind a real `<button>` and build the `<iframe>` on
   click, so the third party is not contacted by visitors who never press play.

`assets/video-poster.png` is already in the repo for this.

## Data

Signups are in the `waitlist` table. Export with the Supabase table editor or
`select email, twitter, source, created_at from waitlist order by created_at`.
