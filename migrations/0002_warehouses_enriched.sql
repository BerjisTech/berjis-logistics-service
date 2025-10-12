-- Enrich warehouses with ownership, coordinates, kind, state, pricing
ALTER TABLE warehouses
  ADD COLUMN IF NOT EXISTS owner_user_id UUID,
  ADD COLUMN IF NOT EXISTS lat DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS lng DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS kind TEXT,
  ADD COLUMN IF NOT EXISTS is_multi_unit BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS state TEXT NOT NULL DEFAULT 'available',
  ADD COLUMN IF NOT EXISTS price_amount NUMERIC,
  ADD COLUMN IF NOT EXISTS price_unit TEXT,
  ADD COLUMN IF NOT EXISTS pricing_mode TEXT,
  ADD COLUMN IF NOT EXISTS area_sqm NUMERIC;

-- Staff assignments per warehouse
CREATE TABLE IF NOT EXISTS warehouse_staff (
  warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
  user_id UUID NOT NULL,
  role TEXT NOT NULL DEFAULT 'staff', -- admin | staff | viewer
  PRIMARY KEY (warehouse_id, user_id)
);

-- Units within a warehouse (shelves/rooms/sections)
CREATE TABLE IF NOT EXISTS warehouse_units (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  area_sqm NUMERIC,
  kind TEXT,
  state TEXT NOT NULL DEFAULT 'available', -- available | occupied | maintenance
  price_amount NUMERIC,
  price_unit TEXT,
  pricing_mode TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

