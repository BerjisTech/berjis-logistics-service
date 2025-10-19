-- Proof of Delivery fields and light shipment helpers
ALTER TABLE IF EXISTS deliveries
  ADD COLUMN IF NOT EXISTS proof_signed_by TEXT,
  ADD COLUMN IF NOT EXISTS proof_photo_url TEXT,
  ADD COLUMN IF NOT EXISTS proof_ts TIMESTAMPTZ;

-- Index for querying active deliveries by vehicle
CREATE INDEX IF NOT EXISTS idx_deliveries_vehicle_active ON deliveries(vehicle_id) WHERE status <> 'completed';

-- Ensure route stop sequence integrity per delivery
CREATE UNIQUE INDEX IF NOT EXISTS ux_route_stops_delivery_seq ON route_stops(delivery_id, seq);