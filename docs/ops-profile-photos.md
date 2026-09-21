# Profile photos and cabal pictures (Supabase Storage)

Monaco stores profile photos and cabal pictures in the hosted **monaco-dev** Supabase project (`komoccjaeypsrgocrnht`). Local Postgres stays on Docker Compose; only object storage uses Supabase.

## Environment

Backend reads (from `.env.local` via `scripts/with-dotenv-local.sh`):

- `SUPABASE_URL` — e.g. `https://komoccjaeypsrgocrnht.supabase.co`
- `SUPABASE_SERVICE_ROLE_KEY` — service role key from Project Settings → API

Do not commit secrets. Do not point `DATABASE_URL` at hosted Supabase.

Without both variables the upload routes answer 503 and nothing is written.

## Storage bucket

Bucket name: **`avatars`** (public read for display URLs). Cabal pictures share it.

Create once on the dev project (dashboard or API):

1. [Storage](https://supabase.com/dashboard/project/komoccjaeypsrgocrnht/storage/buckets) → New bucket → `avatars`
2. Enable **Public bucket** so clients can load `{SUPABASE_URL}/storage/v1/object/public/avatars/...`
3. Uploads go through the Go API with the service role key (mobile never holds storage secrets)

Object keys:

- Profile photos: `{user_id}/{random}.{jpg|png}`
- Cabal pictures: `groups/{group_id}/{random}.{jpg|png}`

## Routes

| Route | What it does |
|---|---|
| `POST /v1/me/profile-photo` | Set the member's photo. Multipart field `photo`. |
| `POST /v1/groups/{id}/picture` | Set or replace the cabal picture. Multipart field `picture`. Creator only. |
| `DELETE /v1/groups/{id}/picture` | Remove the cabal picture; the app falls back to the tinted initials. Creator only. |

Both picture routes answer `{"groupId": "...", "pictureUrl": "..." | null}`. A member who did not create the cabal gets 403; anyone who is not a member (or a made-up id) gets 404. Faker demo clubs refuse every write.

`pictureUrl` (null when unset) is carried by `GET /v1/groups/{id}`, `GET /v1/groups/{id}/view` (with `isCreator`), `GET /v1/groups/search`, `GET /v1/groups/leaderboard`, `GET /v1/home`, `GET /v1/home/dashboard` (`myGroups`) and `GET /v1/users/{id}/groups`. Column: `groups.picture_url` (migration `000019_groups_picture_url.sql`).

## Limits

One pipeline (`internal/imageupload`, wrapped by `app.imageStore`) checks every upload:

- At most 2MB. Over that: **413** on both routes.
- jpeg, png or webp, decided from the bytes, not the declared type. Anything else, or bytes that do not decode: 400.
- At most 4096px on a side and 4096×4096 pixels, read from the header before anything is decoded, so a small file that decodes to gigabytes is refused: 400.
- The server re-encodes from the decoded pixels, so EXIF and anything appended to the file are dropped. Pictures are scaled to a 512px longest side. A source with transparency is stored as PNG; anything opaque becomes JPEG.
- Per-user rate limit: 3 writes at once, then one per 20 s, separately for profile photos and cabal pictures. Past that the API returns 429 with `Retry-After` and nothing is written to storage.
- Each upload writes a new object, so a replaced picture is a new URL and never a stale cache hit. Older objects are not deleted yet.

## Verify

- `GET /v1/me`, `PATCH /v1/me`, and `POST /v1/auth/session` include `profilePhotoUrl` (null when unset) and `createdAt`
- Profile tab (or Settings → Profile photo) → tap the avatar, pick a photo, toast "Profile photo updated."
- Home leaderboard and cabal member boards show the photo after the app refreshes
- As a cabal's creator, open the cabal, tap the mark in the header, pick a photo: toast "Cabal picture updated." Long-press the mark → "Remove picture" brings the initials back
- As a member who did not create the cabal, the header mark has no camera badge and does nothing on tap
- The picture shows on the Cabals strip, search results, the leaderboard, Home "your cabals" and another member's shared cabals after a refresh
