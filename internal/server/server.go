package server

import (
    "github.com/jmoiron/sqlx"
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/cors"
)

type Options struct {
    AllowedOrigins string
    DB             *sqlx.DB
}

type Warehouse struct {
    ID           string   `db:"id" json:"id"`
    Name         string   `db:"name" json:"name"`
    Location     string   `db:"location" json:"location"`
    OwnerUserID  *string  `db:"owner_user_id" json:"ownerUserId,omitempty"`
    Lat          *float64 `db:"lat" json:"lat,omitempty"`
    Lng          *float64 `db:"lng" json:"lng,omitempty"`
    Kind         *string  `db:"kind" json:"kind,omitempty"`
    IsMultiUnit  bool     `db:"is_multi_unit" json:"isMultiUnit"`
    State        string   `db:"state" json:"state"`
    PriceAmount  *float64 `db:"price_amount" json:"priceAmount,omitempty"`
    PriceUnit    *string  `db:"price_unit" json:"priceUnit,omitempty"`
    PricingMode  *string  `db:"pricing_mode" json:"pricingMode,omitempty"`
    AreaSqm      *float64 `db:"area_sqm" json:"areaSqm,omitempty"`
}

type InventoryItem struct {
    ID          string `db:"id" json:"id"`
    WarehouseID string `db:"warehouse_id" json:"warehouseId"`
    SKU         string `db:"sku" json:"sku"`
    Name        string `db:"name" json:"name"`
    Quantity    int    `db:"quantity" json:"quantity"`
}

func New(opts Options) *fiber.App {
    app := fiber.New()
    app.Use(cors.New(cors.Config{
        AllowOrigins:     opts.AllowedOrigins,
        AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
        AllowHeaders:     "Authorization,Content-Type,Accept",
        AllowCredentials: true,
    }))

    // Health
    app.Get("/v1/health", func(c *fiber.Ctx) error {
        return c.JSON(fiber.Map{"success": true, "message": "ok"})
    })

    // Warehouses list
    app.Get("/v1/warehouses", func(c *fiber.Ctx) error {
        rows := []Warehouse{}
        if opts.DB != nil {
            if err := opts.DB.Select(&rows, `SELECT id, name, COALESCE(location, '') AS location,
                owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, price_unit, pricing_mode, area_sqm
              FROM warehouses ORDER BY name`); err != nil {
                return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to list"})
            }
        }
        return c.JSON(fiber.Map{"success": true, "data": rows})
    })

    // Warehouse get
    app.Get("/v1/warehouses/:id", func(c *fiber.Ctx) error {
        id := c.Params("id")
        var w Warehouse
        if opts.DB == nil { return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"}) }
        if err := opts.DB.Get(&w, `SELECT id, name, COALESCE(location, '') AS location,
            owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, price_unit, pricing_mode, area_sqm
          FROM warehouses WHERE id=$1`, id); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
        }
        return c.JSON(fiber.Map{"success": true, "data": w})
    })

    type warehouseIn struct {
        Name        string   `json:"name"`
        Location    string   `json:"location"`
        Lat         *float64 `json:"lat"`
        Lng         *float64 `json:"lng"`
        Kind        *string  `json:"kind"`
        IsMultiUnit *bool    `json:"isMultiUnit"`
        State       *string  `json:"state"`
        PriceAmount *float64 `json:"priceAmount"`
        PriceUnit   *string  `json:"priceUnit"`
        PricingMode *string  `json:"pricingMode"`
        AreaSqm     *float64 `json:"areaSqm"`
    }

    // Warehouse create (requires X-User-ID header for now)
    app.Post("/v1/warehouses", func(c *fiber.Ctx) error {
        userID := c.Get("X-User-ID")
        if userID == "" { return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false, "message": "login required"}) }
        var in warehouseIn
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        if in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "name required"}) }
        var w Warehouse
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if err := opts.DB.Get(&w, `INSERT INTO warehouses
            (name, location, owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, price_unit, pricing_mode, area_sqm)
            VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, COALESCE($7,false), COALESCE($8,'available'), $9, $10, $11, $12)
            RETURNING id, name, COALESCE(location,''::text) AS location, owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, price_unit, pricing_mode, area_sqm`,
            in.Name, in.Location, userID, in.Lat, in.Lng, in.Kind, in.IsMultiUnit, in.State, in.PriceAmount, in.PriceUnit, in.PricingMode, in.AreaSqm); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "create failed"})
        }
        // Owner becomes admin staff
        _, _ = opts.DB.Exec(`INSERT INTO warehouse_staff (warehouse_id, user_id, role) VALUES ($1,$2,'admin') ON CONFLICT DO NOTHING`, w.ID, userID)
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": w})
    })

    // Warehouse update
    app.Put("/v1/warehouses/:id", func(c *fiber.Ctx) error {
        id := c.Params("id")
        var in warehouseIn
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        if in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "name required"}) }
        var w Warehouse
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if err := opts.DB.Get(&w, `UPDATE warehouses SET
            name=$1,
            location=NULLIF($2,''),
            lat=$3, lng=$4, kind=$5, is_multi_unit=COALESCE($6,is_multi_unit), state=COALESCE($7,state),
            price_amount=$8, price_unit=$9, pricing_mode=$10, area_sqm=$11,
            updated_at=now()
          WHERE id=$12
          RETURNING id, name, COALESCE(location,'') AS location, owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, price_unit, pricing_mode, area_sqm`,
          in.Name, in.Location, in.Lat, in.Lng, in.Kind, in.IsMultiUnit, in.State, in.PriceAmount, in.PriceUnit, in.PricingMode, in.AreaSqm, id); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
        }
        return c.JSON(fiber.Map{"success": true, "data": w})
    })

    // Warehouse delete
    app.Delete("/v1/warehouses/:id", func(c *fiber.Ctx) error {
        id := c.Params("id")
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        res, err := opts.DB.Exec(`DELETE FROM warehouses WHERE id=$1`, id)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        n, _ := res.RowsAffected()
        if n == 0 { return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"}) }
        return c.JSON(fiber.Map{"success": true})
    })

    // Inventory: list by warehouse
    app.Get("/v1/warehouses/:id/inventory", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        out := []InventoryItem{}
        if err := opts.DB.Select(&out, `SELECT id, warehouse_id, sku, name, quantity FROM inventory WHERE warehouse_id=$1 ORDER BY sku`, wid); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to list"})
        }
        return c.JSON(fiber.Map{"success": true, "data": out})
    })

    // Staff: list
    app.Get("/v1/warehouses/:id/staff", func(c *fiber.Ctx) error {
        type staffRow struct { UserID string `db:"user_id" json:"userId"`; Role string `db:"role" json:"role"` }
        wid := c.Params("id")
        out := []staffRow{}
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if err := opts.DB.Select(&out, `SELECT user_id, role FROM warehouse_staff WHERE warehouse_id=$1 ORDER BY role`, wid); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": out})
    })

    // Staff: add/update role
    app.Post("/v1/warehouses/:id/staff", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        var in struct { UserID string `json:"userId"`; Role string `json:"role"` }
        if err := c.BodyParser(&in); err != nil || in.UserID == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        _, err := opts.DB.Exec(`INSERT INTO warehouse_staff (warehouse_id, user_id, role)
          VALUES ($1,$2,COALESCE(NULLIF($3,''),'staff'))
          ON CONFLICT (warehouse_id, user_id) DO UPDATE SET role=EXCLUDED.role`, wid, in.UserID, in.Role)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })

    // Staff: remove
    app.Delete("/v1/warehouses/:id/staff/:user", func(c *fiber.Ctx) error {
        wid := c.Params("id"); uid := c.Params("user")
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        _, err := opts.DB.Exec(`DELETE FROM warehouse_staff WHERE warehouse_id=$1 AND user_id=$2`, wid, uid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })

    // Units: list
    app.Get("/v1/warehouses/:id/units", func(c *fiber.Ctx) error {
        type unit struct {
            ID string `db:"id" json:"id"`
            Name string `db:"name" json:"name"`
            AreaSqm *float64 `db:"area_sqm" json:"areaSqm,omitempty"`
            Kind *string `db:"kind" json:"kind,omitempty"`
            State string `db:"state" json:"state"`
            PriceAmount *float64 `db:"price_amount" json:"priceAmount,omitempty"`
            PriceUnit *string `db:"price_unit" json:"priceUnit,omitempty"`
            PricingMode *string `db:"pricing_mode" json:"pricingMode,omitempty"`
        }
        out := []unit{}
        wid := c.Params("id")
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if err := opts.DB.Select(&out, `SELECT id, name, area_sqm, kind, state, price_amount, price_unit, pricing_mode FROM warehouse_units WHERE warehouse_id=$1 ORDER BY name`, wid); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": out})
    })

    // Units: create
    app.Post("/v1/warehouses/:id/units", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        var in struct {
            Name string `json:"name"`
            AreaSqm *float64 `json:"areaSqm"`
            Kind *string `json:"kind"`
            State *string `json:"state"`
            PriceAmount *float64 `json:"priceAmount"`
            PriceUnit *string `json:"priceUnit"`
            PricingMode *string `json:"pricingMode"`
        }
        if err := c.BodyParser(&in); err != nil || in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        type unit struct { ID string `db:"id" json:"id"`; Name string `db:"name" json:"name"`; AreaSqm *float64 `db:"area_sqm" json:"areaSqm"`; Kind *string `db:"kind" json:"kind"`; State string `db:"state" json:"state"`; PriceAmount *float64 `db:"price_amount" json:"priceAmount"`; PriceUnit *string `db:"price_unit" json:"priceUnit"`; PricingMode *string `db:"pricing_mode" json:"pricingMode"` }
        var u unit
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if err := opts.DB.Get(&u, `INSERT INTO warehouse_units (warehouse_id, name, area_sqm, kind, state, price_amount, price_unit, pricing_mode)
           VALUES ($1,$2,$3,$4,COALESCE($5,'available'),$6,$7,$8)
           RETURNING id, name, area_sqm, kind, state, price_amount, price_unit, pricing_mode`, wid, in.Name, in.AreaSqm, in.Kind, in.State, in.PriceAmount, in.PriceUnit, in.PricingMode); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": u})
    })

    // Units: update
    app.Put("/v1/warehouses/:id/units/:unit", func(c *fiber.Ctx) error {
        wid := c.Params("id"); uid := c.Params("unit")
        var in struct {
            Name string `json:"name"`
            AreaSqm *float64 `json:"areaSqm"`
            Kind *string `json:"kind"`
            State *string `json:"state"`
            PriceAmount *float64 `json:"priceAmount"`
            PriceUnit *string `json:"priceUnit"`
            PricingMode *string `json:"pricingMode"`
        }
        if err := c.BodyParser(&in); err != nil || in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        type unit struct { ID string `db:"id" json:"id"`; Name string `db:"name" json:"name"`; AreaSqm *float64 `db:"area_sqm" json:"areaSqm"`; Kind *string `db:"kind" json:"kind"`; State string `db:"state" json:"state"`; PriceAmount *float64 `db:"price_amount" json:"priceAmount"`; PriceUnit *string `db:"price_unit" json:"priceUnit"`; PricingMode *string `db:"pricing_mode" json:"pricingMode"` }
        var u unit
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if err := opts.DB.Get(&u, `UPDATE warehouse_units SET name=$1, area_sqm=$2, kind=$3, state=COALESCE($4,state), price_amount=$5, price_unit=$6, pricing_mode=$7, updated_at=now()
          WHERE id=$8 AND warehouse_id=$9
          RETURNING id, name, area_sqm, kind, state, price_amount, price_unit, pricing_mode`, in.Name, in.AreaSqm, in.Kind, in.State, in.PriceAmount, in.PriceUnit, in.PricingMode, uid, wid); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": u})
    })

    // Units: delete
    app.Delete("/v1/warehouses/:id/units/:unit", func(c *fiber.Ctx) error {
        wid := c.Params("id"); uid := c.Params("unit")
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        res, err := opts.DB.Exec(`DELETE FROM warehouse_units WHERE id=$1 AND warehouse_id=$2`, uid, wid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        n, _ := res.RowsAffected(); if n == 0 { return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })

    type inventoryIn struct { SKU string `json:"sku"`; Name string `json:"name"`; Quantity int `json:"quantity"` }

    // Inventory: create item
    app.Post("/v1/warehouses/:id/inventory", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in inventoryIn
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        if in.SKU == "" || in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sku and name required"}) }
        if in.Quantity < 0 { in.Quantity = 0 }
        var it InventoryItem
        if err := opts.DB.Get(&it, `INSERT INTO inventory (warehouse_id, sku, name, quantity) VALUES ($1,$2,$3,$4) RETURNING id, warehouse_id, sku, name, quantity`, wid, in.SKU, in.Name, in.Quantity); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "create failed"})
        }
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": it})
    })

    // Inventory: update item
    app.Put("/v1/warehouses/:id/inventory/:item", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        iid := c.Params("item")
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in inventoryIn
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        if in.SKU == "" || in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sku and name required"}) }
        if in.Quantity < 0 { in.Quantity = 0 }
        var it InventoryItem
        if err := opts.DB.Get(&it, `UPDATE inventory SET sku=$1, name=$2, quantity=$3, updated_at=now() WHERE id=$4 AND warehouse_id=$5 RETURNING id, warehouse_id, sku, name, quantity`, in.SKU, in.Name, in.Quantity, iid, wid); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
        }
        return c.JSON(fiber.Map{"success": true, "data": it})
    })

    // Inventory: delete item
    app.Delete("/v1/warehouses/:id/inventory/:item", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        iid := c.Params("item")
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        res, err := opts.DB.Exec(`DELETE FROM inventory WHERE id=$1 AND warehouse_id=$2`, iid, wid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        n, _ := res.RowsAffected()
        if n == 0 { return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"}) }
        return c.JSON(fiber.Map{"success": true})
    })

    return app
}
