-- Add optional vehicle location fields for public transport search by proximity
ALTER TABLE vehicles
  ADD COLUMN IF NOT EXISTS lat DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS lng DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS last_seen TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_vehicles_location ON vehicles(lat, lng);

