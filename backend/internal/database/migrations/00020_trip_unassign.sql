-- +goose Up
-- A trip can now exist with no bus and/or no driver assigned ("Unassign" /
-- "Bulk Unassign"), the same way a Special Trip/Outing can already exist with
-- no route (migration 00013): nullable, never invented, exempt from the
-- overlap/slot-key constraints on the null side (Postgres already treats NULL
-- as never-equal in both UNIQUE and EXCLUDE constraints, so a NULL bus_id/
-- driver_id row simply never conflicts via trips_bus_no_overlap/
-- trips_bus_slot_key — no constraint changes are needed for that).
ALTER TABLE trips ALTER COLUMN bus_id DROP NOT NULL;
ALTER TABLE trips ALTER COLUMN driver_id DROP NOT NULL;

-- +goose Down
-- Only safe if nothing was actually unassigned in the interim (any NULL row
-- would violate the restored NOT NULL and fail the migration outright, same
-- caveat as every other down-migration in this feature).
ALTER TABLE trips ALTER COLUMN driver_id SET NOT NULL;
ALTER TABLE trips ALTER COLUMN bus_id SET NOT NULL;
