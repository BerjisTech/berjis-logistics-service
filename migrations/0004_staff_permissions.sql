-- Add granular staff permissions to warehouse_staff
ALTER TABLE warehouse_staff
  ADD COLUMN IF NOT EXISTS permissions JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Optional: seed existing admins with broad permissions
UPDATE warehouse_staff
SET permissions = jsonb_build_object(
  'edit_prices', true,
  'edit_availability', true,
  'manage_discounts', true,
  'manage_inventory', true,
  'manage_units', true,
  'manage_staff', true
)
WHERE role = 'admin' AND (permissions IS NULL OR permissions = '{}'::jsonb);

