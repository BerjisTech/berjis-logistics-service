-- Add owner to vehicles for access control
ALTER TABLE vehicles
  ADD COLUMN IF NOT EXISTS owner_user_id UUID;

CREATE INDEX IF NOT EXISTS idx_vehicles_owner ON vehicles(owner_user_id);

