-- Profile photo URL persisted after Supabase Storage upload (backend-mediated).

ALTER TABLE users ADD COLUMN IF NOT EXISTS profile_photo_url text;
