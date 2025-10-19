-- Transport jobs table for driver marketplace
CREATE TABLE IF NOT EXISTS transport_jobs (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  created_by_user_id UUID,
  status TEXT NOT NULL DEFAULT 'posted', -- posted | assigned | in_transit | completed | cancelled
  pickup_address TEXT,
  pickup_lat DOUBLE PRECISION,
  pickup_lng DOUBLE PRECISION,
  delivery_address TEXT,
  delivery_lat DOUBLE PRECISION,
  delivery_lng DOUBLE PRECISION,
  cargo_desc TEXT,
  vehicle_kind TEXT,
  capacity_kg NUMERIC,
  payment NUMERIC,
  currency TEXT DEFAULT 'USD',
  scheduled_pickup TIMESTAMPTZ,
  driver_id UUID REFERENCES drivers(id) ON DELETE SET NULL,
  vehicle_id UUID REFERENCES vehicles(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_transport_jobs_status ON transport_jobs(status);
CREATE INDEX IF NOT EXISTS idx_transport_jobs_driver ON transport_jobs(driver_id);