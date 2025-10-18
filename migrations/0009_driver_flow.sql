-- Enhance drivers for role flow and permissions
ALTER TABLE drivers
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending', -- pending | verified | suspended
  ADD COLUMN IF NOT EXISTS permissions JSONB NOT NULL DEFAULT '{}'::jsonb;

