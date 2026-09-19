# Profile photos (Supabase Storage)

Monaco stores profile photos in the hosted **monaco-dev** Supabase project (`komoccjaeypsrgocrnht`). Local Postgres stays on Docker Compose; only object storage uses Supabase.

## Environment

Backend reads (from `.env.local` via `scripts/with-dotenv-local.sh`):

- `SUPABASE_URL` — e.g. `https://komoccjaeypsrgocrnht.supabase.co`
- `SUPABASE_SERVICE_ROLE_KEY` — service role key from Project Settings → API

Do not commit secrets. Do not point `DATABASE_URL` at hosted Supabase.

## Storage bucket

Bucket name: **`avatars`** (public read for display URLs).

Create once on the dev project (dashboard or API):

1. [Storage](https://supabase.com/dashboard/project/komoccjaeypsrgocrnht/storage/buckets) → New bucket → `avatars`
2. Enable **Public bucket** so clients can load `{SUPABASE_URL}/storage/v1/object/public/avatars/...`
3. Uploads go through the Go API with the service role key (mobile never holds storage secrets)

Object keys: `{user_id}/{random}.{jpg|png|webp}`.

## Limits

- Multipart field `photo`, at most 2MB, jpeg/png/webp (checked by magic bytes, not the declared type). The app downscales and re-encodes to JPEG before upload.
- Per-user rate limit: 3 uploads at once, then one per 20 s. Past that the API returns 429 with `Retry-After` and nothing is written to storage.
- Each upload writes a new object; older objects for the user are not deleted yet.

## Verify

- `GET /v1/me`, `PATCH /v1/me`, and `POST /v1/auth/session` include `profilePhotoUrl` (null when unset) and `createdAt`
- `POST /v1/me/profile-photo` multipart field `photo`, max ~2MB, jpeg/png/webp
- Profile tab (or Settings → Profile photo) → tap the avatar, pick a photo, toast "Profile photo updated."
- Home leaderboard and cabal member boards show the photo after the app refreshes
