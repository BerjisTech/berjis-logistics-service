-- Align warehouse pricing terminology with frontend
ALTER TABLE warehouses
  RENAME COLUMN price_unit TO currency;

ALTER TABLE warehouse_units
  RENAME COLUMN price_unit TO currency;

-- Optional safety: ensure the column exists after rename (for idempotency on re-runs)
ALTER TABLE warehouses
  ADD COLUMN IF NOT EXISTS currency TEXT;

ALTER TABLE warehouse_units
  ADD COLUMN IF NOT EXISTS currency TEXT;
