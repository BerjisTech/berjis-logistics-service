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
                // Return empty list on error to keep UI running in early dev
                return c.JSON(fiber.Map{"success": true, "data": []any{}})
            }
        }
        return c.JSON(fiber.Map{"success": true, "data": rows})
    })

    return app
}
