package server

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/berjistech/berjis-ecosystem/logistics/service/internal/auth"
)

type Warehouse struct {
	ID          string   `db:"id" json:"id"`
	Name        string   `db:"name" json:"name"`
	Location    string   `db:"location" json:"location"`
	OwnerUserID *string  `db:"owner_user_id" json:"ownerUserId,omitempty"`
	Lat         *float64 `db:"lat" json:"lat,omitempty"`
	Lng         *float64 `db:"lng" json:"lng,omitempty"`
	Kind        *string  `db:"kind" json:"kind,omitempty"`
	IsMultiUnit bool     `db:"is_multi_unit" json:"isMultiUnit"`
	State       string   `db:"state" json:"state"`
	PriceAmount *float64 `db:"price_amount" json:"priceAmount,omitempty"`
	Currency    *string  `db:"currency" json:"currency,omitempty"`
	PricingMode *string  `db:"pricing_mode" json:"pricingMode,omitempty"`
	AreaSqm     *float64 `db:"area_sqm" json:"areaSqm,omitempty"`
}

type InventoryItem struct {
	ID          string `db:"id" json:"id"`
	WarehouseID string `db:"warehouse_id" json:"warehouseId"`
	SKU         string `db:"sku" json:"sku"`
	Name        string `db:"name" json:"name"`
	Quantity    int    `db:"quantity" json:"quantity"`
}

type Booking struct {
	ID            string   `db:"id" json:"id"`
	WarehouseID   string   `db:"warehouse_id" json:"warehouseId"`
	TenantUserID  string   `db:"tenant_user_id" json:"tenantUserId"`
	SpaceReserved *float64 `db:"space_reserved" json:"spaceReserved,omitempty"`
	StartDate     string   `db:"start_date" json:"startDate"`
	EndDate       *string  `db:"end_date" json:"endDate,omitempty"`
	PricePerDay   *float64 `db:"price_per_day" json:"pricePerDay,omitempty"`
	Status        string   `db:"status" json:"status"`
}

type warehouseIn struct {
	Name        string   `json:"name"`
	Location    string   `json:"location"`
	Lat         *float64 `json:"lat"`
	Lng         *float64 `json:"lng"`
	Kind        *string  `json:"kind"`
	IsMultiUnit *bool    `json:"isMultiUnit"`
	State       *string  `json:"state"`
	PriceAmount *float64 `json:"priceAmount"`
	Currency    *string  `json:"currency"`
	PricingMode *string  `json:"pricingMode"`
	AreaSqm     *float64 `json:"areaSqm"`
}

type warehouseUnitRow struct {
	ID          string   `db:"id" json:"id"`
	Name        string   `db:"name" json:"name"`
	AreaSqm     *float64 `db:"area_sqm" json:"areaSqm,omitempty"`
	Kind        *string  `db:"kind" json:"kind,omitempty"`
	State       string   `db:"state" json:"state"`
	PriceAmount *float64 `db:"price_amount" json:"priceAmount,omitempty"`
	Currency    *string  `db:"currency" json:"currency,omitempty"`
	PricingMode *string  `db:"pricing_mode" json:"pricingMode,omitempty"`
}

type warehouseUnitIn struct {
	Name        string   `json:"name"`
	AreaSqm     *float64 `json:"areaSqm"`
	Kind        *string  `json:"kind"`
	State       *string  `json:"state"`
	PriceAmount *float64 `json:"priceAmount"`
	Currency    *string  `json:"currency"`
	PricingMode *string  `json:"pricingMode"`
}

type inventoryIn struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type staffRow struct {
	UserID      string         `db:"user_id" json:"userId"`
	Role        string         `db:"role" json:"role"`
	Permissions map[string]any `db:"permissions" json:"permissions"`
}

type staffIn struct {
	UserID      string         `json:"userId"`
	Role        string         `json:"role"`
	Permissions map[string]any `json:"permissions"`
}

type bookingIn struct {
	StartDate     string   `json:"startDate"`
	EndDate       *string  `json:"endDate"`
	SpaceReserved *float64 `json:"spaceReserved"`
}

func registerStorageRoutes(app *fiber.App, opts Options) {
	// Warehouses list
	app.Get("/v1/warehouses", func(c *fiber.Ctx) error {
		rows := []Warehouse{}
		if opts.DB != nil {
			uid := auth.UserID(c)
			if err := opts.DB.Select(&rows, `SELECT w.id, w.name, COALESCE(w.location, '') AS location,
                w.owner_user_id, w.lat, w.lng, w.kind, w.is_multi_unit, w.state,
                w.price_amount, w.currency, w.pricing_mode, w.area_sqm
              FROM warehouses w
              WHERE w.owner_user_id = $1 OR EXISTS (
                SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id = w.id AND s.user_id = $1
              )
              ORDER BY w.name`, uid); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to list warehouses"})
			}
		}
		return c.JSON(fiber.Map{"success": true, "data": rows})
	})

	// Warehouse get
	app.Get("/v1/warehouses/:id", func(c *fiber.Ctx) error {
		id := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
		}
		var w Warehouse
		if err := opts.DB.Get(&w, `SELECT id, name, COALESCE(location, '') AS location,
            owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, currency, pricing_mode, area_sqm
          FROM warehouses
          WHERE id=$1 AND (
            owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff WHERE warehouse_id=$1 AND user_id=$2
            )
          )`, id, uid); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
		}
		return c.JSON(fiber.Map{"success": true, "data": w})
	})

	// Warehouse create
	app.Post("/v1/warehouses", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		userID := auth.UserID(c)
		var in warehouseIn
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "name required"})
		}
		var w Warehouse
		if err := opts.DB.Get(&w, `INSERT INTO warehouses
            (name, location, owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, currency, pricing_mode, area_sqm)
            VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, COALESCE($7,false), COALESCE($8,'available'), $9, $10, $11, $12)
            RETURNING id, name, COALESCE(location,''::text) AS location, owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, currency, pricing_mode, area_sqm`,
			in.Name, in.Location, userID, in.Lat, in.Lng, in.Kind, in.IsMultiUnit, in.State, in.PriceAmount, in.Currency, in.PricingMode, in.AreaSqm); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "create failed"})
		}
		_, _ = opts.DB.Exec(`INSERT INTO warehouse_staff (warehouse_id, user_id, role) VALUES ($1,$2,'admin') ON CONFLICT DO NOTHING`, w.ID, userID)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": w})
	})

	// Warehouse update
	app.Put("/v1/warehouses/:id", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		uid := auth.UserID(c)
		var in warehouseIn
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "name required"})
		}

		priceChange := (in.PriceAmount != nil) || (in.Currency != nil) || (in.PricingMode != nil)
		stateChange := in.State != nil
		allowed := false
		if isOwnerOrAdmin(opts.DB, id, uid) {
			allowed = true
		} else if priceChange && hasPermission(opts.DB, id, uid, "edit_prices") {
			allowed = true
		} else if stateChange && hasPermission(opts.DB, id, uid, "edit_availability") {
			allowed = true
		}
		if !allowed {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}

		var w Warehouse
		if err := opts.DB.Get(&w, `UPDATE warehouses SET
            name=$1,
            location=NULLIF($2,''),
            lat=$3,
            lng=$4,
            kind=$5,
            is_multi_unit=COALESCE($6,is_multi_unit),
            state=COALESCE($7,state),
            price_amount=$8,
            currency=$9,
            pricing_mode=$10,
            area_sqm=$11,
            updated_at=NOW()
          WHERE id=$12
          RETURNING id, name, COALESCE(location,'') AS location, owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, currency, pricing_mode, area_sqm`,
			in.Name, in.Location, in.Lat, in.Lng, in.Kind, in.IsMultiUnit, in.State, in.PriceAmount, in.Currency, in.PricingMode, in.AreaSqm, id); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
		}
		return c.JSON(fiber.Map{"success": true, "data": w})
	})

	// Warehouse delete
	app.Delete("/v1/warehouses/:id", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		uid := auth.UserID(c)
		res, err := opts.DB.Exec(`DELETE FROM warehouses WHERE id=$1 AND (
            owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff WHERE warehouse_id=$1 AND user_id=$2 AND role IN ('admin')
            )
          )`, id, uid)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
		}
		return c.JSON(fiber.Map{"success": true})
	})

	// Staff: list
	app.Get("/v1/warehouses/:id/staff", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		if !(isOwnerOrAdmin(opts.DB, wid, uid) || hasPermission(opts.DB, wid, uid, "manage_staff")) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		out := []staffRow{}
		if err := opts.DB.Select(&out, `SELECT user_id, role, COALESCE(permissions,'{}'::jsonb) AS permissions FROM warehouse_staff WHERE warehouse_id=$1 ORDER BY role`, wid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": out})
	})

	// Staff: add/update
	app.Post("/v1/warehouses/:id/staff", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		if !(isOwnerOrAdmin(opts.DB, wid, uid) || hasPermission(opts.DB, wid, uid, "manage_staff")) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var in staffIn
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.UserID) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		if in.Role == "" {
			in.Role = "staff"
		}
		if _, err := opts.DB.Exec(`INSERT INTO warehouse_staff (warehouse_id, user_id, role, permissions)
          VALUES ($1,$2,COALESCE(NULLIF($3,''),'staff'), COALESCE($4,'{}'::jsonb))
          ON CONFLICT (warehouse_id, user_id) DO UPDATE SET role=EXCLUDED.role, permissions=COALESCE(EXCLUDED.permissions,'{}'::jsonb)`,
			wid, in.UserID, in.Role, in.Permissions); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true})
	})

	// Staff: remove
	app.Delete("/v1/warehouses/:id/staff/:user", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		member := c.Params("user")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		if !(isOwnerOrAdmin(opts.DB, wid, uid) || hasPermission(opts.DB, wid, uid, "manage_staff")) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		if _, err := opts.DB.Exec(`DELETE FROM warehouse_staff WHERE warehouse_id=$1 AND user_id=$2`, wid, member); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true})
	})

	// Units: list (must have access)
	app.Get("/v1/warehouses/:id/units", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var allowed bool
		if err := opts.DB.Get(&allowed, `SELECT EXISTS (
            SELECT 1 FROM warehouses w WHERE w.id=$1 AND (w.owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=w.id AND s.user_id=$2
            ))
          )`, wid, uid); err != nil || !allowed {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		out := []warehouseUnitRow{}
		if err := opts.DB.Select(&out, `SELECT id, name, area_sqm, kind, state, price_amount, currency, pricing_mode FROM warehouse_units WHERE warehouse_id=$1 ORDER BY name`, wid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": out})
	})

	// Units: create (admin/owner or manage_units)
	app.Post("/v1/warehouses/:id/units", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		if !(isOwnerOrAdmin(opts.DB, wid, uid) || hasPermission(opts.DB, wid, uid, "manage_units")) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var in warehouseUnitIn
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		var row warehouseUnitRow
		if err := opts.DB.Get(&row, `INSERT INTO warehouse_units (warehouse_id, name, area_sqm, kind, state, price_amount, currency, pricing_mode)
           VALUES ($1,$2,$3,$4,COALESCE($5,'available'),$6,$7,$8)
           RETURNING id, name, area_sqm, kind, state, price_amount, currency, pricing_mode`,
			wid, in.Name, in.AreaSqm, in.Kind, in.State, in.PriceAmount, in.Currency, in.PricingMode); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": row})
	})

	// Units: update (admin/owner or manage_units)
	app.Put("/v1/warehouses/:id/units/:unit", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		unitID := c.Params("unit")
		user := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var in warehouseUnitIn
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		var row warehouseUnitRow
		if err := opts.DB.Get(&row, `UPDATE warehouse_units SET
            name=$1,
            area_sqm=$2,
            kind=$3,
            state=COALESCE($4,state),
            price_amount=$5,
            currency=$6,
            pricing_mode=$7,
            updated_at=NOW()
          WHERE id=$8 AND warehouse_id=$9 AND (
            EXISTS (
              SELECT 1 FROM warehouses w WHERE w.id=$9 AND (
                w.owner_user_id=$10 OR EXISTS (
                  SELECT 1 FROM warehouse_staff s
                  WHERE s.warehouse_id=$9 AND s.user_id=$10 AND (s.role IN ('admin') OR (s.permissions->>'manage_units')='true')
                )
              )
            )
          )
          RETURNING id, name, area_sqm, kind, state, price_amount, currency, pricing_mode`,
			in.Name, in.AreaSqm, in.Kind, in.State, in.PriceAmount, in.Currency, in.PricingMode, unitID, wid, user); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": row})
	})

	// Units: delete (admin/owner or manage_units)
	app.Delete("/v1/warehouses/:id/units/:unit", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		unitID := c.Params("unit")
		user := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		res, err := opts.DB.Exec(`DELETE FROM warehouse_units WHERE id=$1 AND warehouse_id=$2 AND (
            EXISTS (SELECT 1 FROM warehouses w WHERE w.id=$2 AND (
              w.owner_user_id=$3 OR EXISTS (
                SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=$2 AND s.user_id=$3 AND (s.role IN ('admin') OR (s.permissions->>'manage_units')='true')
              )
            ))
          )`, unitID, wid, user)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true})
	})

	// Inventory: list (must have access)
	app.Get("/v1/warehouses/:id/inventory", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var allowed bool
		if err := opts.DB.Get(&allowed, `SELECT EXISTS (
            SELECT 1 FROM warehouses w WHERE w.id=$1 AND (w.owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=w.id AND s.user_id=$2
            ))
          )`, wid, uid); err != nil || !allowed {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		out := []InventoryItem{}
		if err := opts.DB.Select(&out, `SELECT id, warehouse_id, sku, name, quantity FROM inventory WHERE warehouse_id=$1 ORDER BY sku`, wid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to list"})
		}
		return c.JSON(fiber.Map{"success": true, "data": out})
	})

	// Inventory: create (admin/owner or manage_inventory)
	app.Post("/v1/warehouses/:id/inventory", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		user := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var in inventoryIn
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.SKU) == "" || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sku and name required"})
		}
		if in.Quantity < 0 {
			in.Quantity = 0
		}
		if !(isOwnerOrAdmin(opts.DB, wid, user) || hasPermission(opts.DB, wid, user, "manage_inventory")) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var item InventoryItem
		if err := opts.DB.Get(&item, `INSERT INTO inventory (warehouse_id, sku, name, quantity)
          VALUES ($1,$2,$3,$4)
          RETURNING id, warehouse_id, sku, name, quantity`, wid, in.SKU, in.Name, in.Quantity); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "create failed"})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": item})
	})

	// Inventory: update (admin/owner or manage_inventory)
	app.Put("/v1/warehouses/:id/inventory/:item", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		itemID := c.Params("item")
		user := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var in inventoryIn
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.SKU) == "" || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sku and name required"})
		}
		if in.Quantity < 0 {
			in.Quantity = 0
		}
		var item InventoryItem
		if err := opts.DB.Get(&item, `UPDATE inventory SET
            sku=$1,
            name=$2,
            quantity=$3,
            updated_at=NOW()
          WHERE id=$4 AND warehouse_id=$5 AND (
            EXISTS (SELECT 1 FROM warehouses w WHERE w.id=$5 AND (
              w.owner_user_id=$6 OR EXISTS (
                SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=$5 AND s.user_id=$6 AND (s.role IN ('admin') OR (s.permissions->>'manage_inventory')='true')
              )
            ))
          )
          RETURNING id, warehouse_id, sku, name, quantity`,
			in.SKU, in.Name, in.Quantity, itemID, wid, user); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
		}
		return c.JSON(fiber.Map{"success": true, "data": item})
	})

	// Inventory: delete (admin/owner or manage_inventory)
	app.Delete("/v1/warehouses/:id/inventory/:item", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		itemID := c.Params("item")
		user := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		res, err := opts.DB.Exec(`DELETE FROM inventory WHERE id=$1 AND warehouse_id=$2 AND (
            EXISTS (SELECT 1 FROM warehouses w WHERE w.id=$2 AND (
              w.owner_user_id=$3 OR EXISTS (
                SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=$2 AND s.user_id=$3 AND (s.role IN ('admin') OR (s.permissions->>'manage_inventory')='true')
              )
            ))
          )`, itemID, wid, user)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
		}
		return c.JSON(fiber.Map{"success": true})
	})

	// Bookings: create
	app.Post("/v1/warehouses/:id/bookings", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var in bookingIn
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.StartDate) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		var allowed bool
		if err := opts.DB.Get(&allowed, `SELECT EXISTS (
            SELECT 1 FROM warehouses w WHERE w.id=$1 AND (w.owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=w.id AND s.user_id=$2
            ))
          )`, wid, uid); err != nil || !allowed {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var b Booking
		if err := opts.DB.Get(&b, `INSERT INTO bookings (warehouse_id, tenant_user_id, space_reserved, start_date, end_date)
          VALUES ($1,$2,$3,$4,$5)
          RETURNING id, warehouse_id, tenant_user_id, space_reserved, start_date, end_date, price_per_day, status`,
			wid, uid, in.SpaceReserved, in.StartDate, in.EndDate); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		broadcastJSON(fmt.Sprintf(`{"kind":"booking","event":"created","id":"%s","warehouseId":"%s","tenantUserId":"%s","startDate":"%s"}`, b.ID, b.WarehouseID, b.TenantUserID, b.StartDate))
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": b})
	})

	// Bookings: list mine
	app.Get("/v1/bookings", func(c *fiber.Ctx) error {
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		out := []Booking{}
		if err := opts.DB.Select(&out, `SELECT id, warehouse_id, tenant_user_id, space_reserved, start_date, end_date, price_per_day, status
          FROM bookings WHERE tenant_user_id=$1 ORDER BY start_date DESC`, uid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": out})
	})

	// Bookings: list for a warehouse (owner/admin)
	app.Get("/v1/warehouses/:id/bookings", func(c *fiber.Ctx) error {
		wid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var allowed bool
		if err := opts.DB.Get(&allowed, `SELECT EXISTS (
            SELECT 1 FROM warehouses w WHERE w.id=$1 AND (w.owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=w.id AND s.user_id=$2 AND s.role IN ('admin')
            ))
          )`, wid, uid); err != nil || !allowed {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		out := []Booking{}
		if err := opts.DB.Select(&out, `SELECT id, warehouse_id, tenant_user_id, space_reserved, start_date, end_date, price_per_day, status
          FROM bookings WHERE warehouse_id=$1 ORDER BY start_date DESC`, wid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": out})
	})

	// Bookings: cancel
	app.Delete("/v1/bookings/:id", func(c *fiber.Ctx) error {
		bid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		res, err := opts.DB.Exec(`UPDATE bookings b SET status='cancelled', updated_at=NOW()
          WHERE b.id=$1 AND (
            b.tenant_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouses w WHERE w.id=b.warehouse_id AND (
                w.owner_user_id=$2 OR EXISTS (
                  SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=w.id AND s.user_id=$2 AND s.role IN ('admin')
                )
              )
            )
          )`, bid, uid)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		broadcastJSON(fmt.Sprintf(`{"kind":"booking","event":"cancelled","id":"%s"}`, bid))
		return c.JSON(fiber.Map{"success": true})
	})
}
