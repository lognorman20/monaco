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

## Verify

- `GET /v1/me` includes `profilePhotoUrl` (null when unset)
- `POST /v1/me/profile-photo` multipart field `photo`, max ~2MB, jpeg/png/webp
- Settings → placeholder, Upload PFP, preview after upload
