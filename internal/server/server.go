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
    ID       string `db:"id" json:"id"`
    Name     string `db:"name" json:"name"`
    Location string `db:"location" json:"location"`
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
            if err := opts.DB.Select(&rows, `SELECT id, name, COALESCE(location, '') AS location FROM warehouses ORDER BY name`); err != nil {
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
        if err := opts.DB.Get(&w, `SELECT id, name, COALESCE(location, '') AS location FROM warehouses WHERE id=$1`, id); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
        }
        return c.JSON(fiber.Map{"success": true, "data": w})
    })

    type warehouseIn struct { Name string `json:"name"`; Location string `json:"location"` }

    // Warehouse create
    app.Post("/v1/warehouses", func(c *fiber.Ctx) error {
        var in warehouseIn
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        if in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "name required"}) }
        var w Warehouse
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if err := opts.DB.Get(&w, `INSERT INTO warehouses (name, location) VALUES ($1, NULLIF($2, '')) RETURNING id, name, COALESCE(location, '') AS location`, in.Name, in.Location); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "create failed"})
        }
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
        if err := opts.DB.Get(&w, `UPDATE warehouses SET name=$1, location=NULLIF($2, ''), updated_at=now() WHERE id=$3 RETURNING id, name, COALESCE(location,'') AS location`, in.Name, in.Location, id); err != nil {
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
