-- Route stop geofencing columns
ALTER TABLE IF EXISTS route_stops
  ADD COLUMN IF NOT EXISTS lat DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS lng DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS arrived_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS departed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_route_stops_delivery_seq ON route_stops(delivery_id, seq);
CREATE INDEX IF NOT EXISTS idx_route_stops_delivery_pending ON route_stops(delivery_id) WHERE arrived_at IS NULL;-- Geofencing on device position inserts
CREATE OR REPLACE FUNCTION geofence_update()
RETURNS trigger AS $$
DECLARE
  v_id uuid := NEW.vehicle_id;
  v_lat double precision := NEW.lat;
  v_lng double precision := NEW.lng;
  stop_id uuid;
BEGIN
  IF v_id IS NULL THEN
    RETURN NEW;
  END IF;
  -- Arrive at next pending stop within 100m
  SELECT rs.id INTO stop_id
  FROM route_stops rs
  JOIN deliveries d ON d.id = rs.delivery_id
  WHERE d.vehicle_id = v_id AND d.status <> 'completed'
    AND rs.arrived_at IS NULL
    AND rs.lat IS NOT NULL AND rs.lng IS NOT NULL
    AND (6371000 * acos( cos(radians(v_lat)) * cos(radians(rs.lat)) * cos(radians(rs.lng) - radians(v_lng)) + sin(radians(v_lat)) * sin(radians(rs.lat)) )) <= 100
  ORDER BY rs.seq ASC
  LIMIT 1;

  IF stop_id IS NOT NULL THEN
    UPDATE route_stops SET arrived_at = NEW.ts, status='arrived'
    WHERE id = stop_id AND arrived_at IS NULL;
  END IF;

  -- Depart when > 150m from current stop
  stop_id := NULL;
  SELECT rs.id INTO stop_id
  FROM route_stops rs
  JOIN deliveries d ON d.id = rs.delivery_id
  WHERE d.vehicle_id = v_id AND d.status <> 'completed'
    AND rs.arrived_at IS NOT NULL AND rs.departed_at IS NULL
    AND rs.lat IS NOT NULL AND rs.lng IS NOT NULL
    AND (6371000 * acos( cos(radians(v_lat)) * cos(radians(rs.lat)) * cos(radians(rs.lng) - radians(v_lng)) + sin(radians(v_lat)) * sin(radians(rs.lat)) )) > 150
  ORDER BY rs.seq ASC
  LIMIT 1;

  IF stop_id IS NOT NULL THEN
    UPDATE route_stops SET departed_at = NEW.ts, status='departed'
    WHERE id = stop_id AND departed_at IS NULL;
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_geofence_on_device_positions ON device_positions;
CREATE TRIGGER trg_geofence_on_device_positions
AFTER INSERT ON device_positions
FOR EACH ROW
WHEN (NEW.vehicle_id IS NOT NULL)
EXECUTE FUNCTION geofence_update();
