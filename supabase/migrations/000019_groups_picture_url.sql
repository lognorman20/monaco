-- Cabal picture URL persisted after Supabase Storage upload (backend-mediated),
-- the group-level counterpart of users.profile_photo_url (000010).
--
-- Nullable: a cabal without a picture renders tinted initials, which stays the
-- default. Clearing a picture sets this back to NULL.

ALTER TABLE groups ADD COLUMN IF NOT EXISTS picture_url text;
