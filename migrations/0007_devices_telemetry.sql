-- Device registry and telemetry positions (future hardware trackers)
CREATE TABLE IF NOT EXISTS devices (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  vehicle_id UUID REFERENCES vehicles(id) ON DELETE SET NULL,
  asset_id UUID,
  name TEXT,
  imei TEXT UNIQUE,
  serial TEXT,
  kind TEXT, -- e.g., teltonika, queclink, app
  secret TEXT NOT NULL, -- store hashed
  status TEXT NOT NULL DEFAULT 'active', -- active | disabled
  last_seen TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_devices_vehicle ON devices(vehicle_id);
CREATE INDEX IF NOT EXISTS idx_devices_asset ON devices(asset_id);

CREATE TABLE IF NOT EXISTS device_positions (
  id BIGSERIAL PRIMARY KEY,
  device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  vehicle_id UUID REFERENCES vehicles(id) ON DELETE SET NULL,
  asset_id UUID,
  lat DOUBLE PRECISION NOT NULL,
  lng DOUBLE PRECISION NOT NULL,
  speed NUMERIC,
  heading NUMERIC,
  altitude NUMERIC,
  hdop NUMERIC,
  sats INTEGER,
  ts TIMESTAMPTZ NOT NULL DEFAULT now(),
  raw JSONB
);
CREATE INDEX IF NOT EXISTS idx_device_positions_device_ts ON device_positions(device_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_device_positions_vehicle_ts ON device_positions(vehicle_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_device_positions_asset_ts ON device_positions(asset_id, ts DESC);
