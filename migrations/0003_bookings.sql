-- Bookings for warehouse space
CREATE TABLE IF NOT EXISTS bookings (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
  tenant_user_id UUID NOT NULL,
  space_reserved NUMERIC,
  start_date DATE NOT NULL,
  end_date DATE,
  price_per_day NUMERIC,
  status TEXT NOT NULL DEFAULT 'pending', -- pending | confirmed | cancelled | completed
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_bookings_tenant ON bookings(tenant_user_id);
CREATE INDEX IF NOT EXISTS idx_bookings_wh ON bookings(warehouse_id);

