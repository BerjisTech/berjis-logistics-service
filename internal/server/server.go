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

    return app
}
