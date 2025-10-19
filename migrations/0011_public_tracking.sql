-- Public tracking for deliveries
ALTER TABLE IF EXISTS deliveries
  ADD COLUMN IF NOT EXISTS tracking_code TEXT,
  ADD COLUMN IF NOT EXISTS tracking_enabled BOOLEAN NOT NULL DEFAULT false;

-- Ensure tracking_code uniqueness when present
CREATE UNIQUE INDEX IF NOT EXISTS ux_deliveries_tracking_code
  ON deliveries((lower(tracking_code)))
  WHERE tracking_code IS NOT NULL;

-- Optional helper view for public tracking summary
CREATE OR REPLACE VIEW public_delivery_summary AS
SELECT 
  d.id AS delivery_id,
  d.status,
  d.tracking_code,
  d.tracking_enabled,
  v.id AS vehicle_id,
  v.plate AS vehicle_plate,
  v.lat AS vehicle_lat,
  v.lng AS vehicle_lng,
  v.updated_at AS vehicle_updated_at
FROM deliveries d
LEFT JOIN vehicles v ON v.id = d.vehicle_id;