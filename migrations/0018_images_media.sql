-- Add images arrays to warehouses and vehicles
ALTER TABLE warehouses
  ADD COLUMN IF NOT EXISTS images JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE vehicles
  ADD COLUMN IF NOT EXISTS images JSONB NOT NULL DEFAULT '[]'::jsonb;

