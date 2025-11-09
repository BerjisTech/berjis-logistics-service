package server

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/berjistech/berjis-ecosystem/logistics/service/internal/auth"
)

func registerStorefrontRoutes(app *fiber.App, opts Options) {
	// Stores: create
	app.Post("/v1/stores", func(c *fiber.Ctx) error {
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var in struct {
			Name        string  `json:"name"`
			Slug        *string `json:"slug"`
			Description *string `json:"description"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "name required"})
		}
		var privileged bool
		_ = opts.DB.Get(&privileged, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role IN ('platform.admin','admin','paid'))`, uid)
		var count int
		_ = opts.DB.Get(&count, `SELECT COUNT(*) FROM stores WHERE owner_user_id=$1`, uid)
		if !privileged && count >= 1 {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false, "message": "store limit reached"})
		}
		slug := ""
		if in.Slug != nil && strings.TrimSpace(*in.Slug) != "" {
			slug = *in.Slug
		} else {
			slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(in.Name), " ", "-"))
		}
		if slug == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid slug"})
		}
		var exists bool
		_ = opts.DB.Get(&exists, `SELECT EXISTS (SELECT 1 FROM stores WHERE LOWER(slug)=LOWER($1))`, slug)
		if exists {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"success": false, "message": "slug taken"})
		}
		var out struct {
			ID   string `db:"id" json:"id"`
			Name string `db:"name" json:"name"`
			Slug string `db:"slug" json:"slug"`
		}
		if err := opts.DB.Get(&out, `INSERT INTO stores (owner_user_id, name, slug, description) VALUES ($1,$2,$3,$4) RETURNING id, name, slug`,
			uid, in.Name, slug, in.Description); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": out})
	})

	// Stores: list mine
	app.Get("/v1/stores", func(c *fiber.Ctx) error {
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		rows := []struct {
			ID          string  `db:"id" json:"id"`
			Name        string  `db:"name" json:"name"`
			Slug        string  `db:"slug" json:"slug"`
			Description *string `db:"description" json:"description"`
		}{}
		if err := opts.DB.Select(&rows, `SELECT id, name, slug, description FROM stores WHERE owner_user_id=$1 ORDER BY created_at DESC`, uid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": rows})
	})

	// Products: create
	app.Post("/v1/products", func(c *fiber.Ctx) error {
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var in struct {
			SKU         *string  `json:"sku"`
			Name        string   `json:"name"`
			Description *string  `json:"description"`
			Price       *float64 `json:"price"`
			Currency    *string  `json:"currency"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		var out struct {
			ID   string  `db:"id" json:"id"`
			Name string  `db:"name" json:"name"`
			SKU  *string `db:"sku" json:"sku"`
		}
		if err := opts.DB.Get(&out, `INSERT INTO products (owner_user_id, sku, name, description, price, currency)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, name, sku`, uid, in.SKU, in.Name, in.Description, in.Price, in.Currency); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": out})
	})

	// Products: list mine
	app.Get("/v1/products", func(c *fiber.Ctx) error {
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		rows := []struct {
			ID       string   `db:"id" json:"id"`
			SKU      *string  `db:"sku" json:"sku"`
			Name     string   `db:"name" json:"name"`
			Price    *float64 `db:"price" json:"price"`
			Currency *string  `db:"currency" json:"currency"`
		}{}
		if err := opts.DB.Select(&rows, `SELECT id, sku, name, price, currency FROM products WHERE owner_user_id=$1 ORDER BY created_at DESC`, uid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": rows})
	})

	// Store: list assigned products (owner/admin)
	app.Get("/v1/stores/:id/products", func(c *fiber.Ctx) error {
		sid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var allowed bool
		_ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM stores WHERE id=$1 AND owner_user_id=$2)`, sid, uid)
		if !allowed {
			_ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role IN ('platform.admin','admin'))`, uid)
		}
		if !allowed {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false})
		}
		rows := []struct {
			ID            string   `db:"id" json:"id"`
			Name          string   `db:"name" json:"name"`
			SKU           *string  `db:"sku" json:"sku"`
			Price         *float64 `db:"price" json:"price"`
			Currency      *string  `db:"currency" json:"currency"`
			PriceOverride *float64 `db:"price_override" json:"priceOverride"`
		}{}
		if err := opts.DB.Select(&rows, `SELECT p.id, p.name, p.sku, p.price, p.currency, sp.price_override
			FROM store_products sp JOIN products p ON p.id = sp.product_id
			WHERE sp.store_id=$1 ORDER BY p.name`, sid); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": rows})
	})

	// Store: assign product
	app.Post("/v1/stores/:id/products", func(c *fiber.Ctx) error {
		sid := c.Params("id")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var allowed bool
		_ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM stores WHERE id=$1 AND owner_user_id=$2)`, sid, uid)
		if !allowed {
			_ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role IN ('platform.admin','admin'))`, uid)
		}
		if !allowed {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false})
		}
		var in struct {
			ProductID     string   `json:"productId"`
			PriceOverride *float64 `json:"priceOverride"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.ProductID) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		_, err := opts.DB.Exec(`INSERT INTO store_products (store_id, product_id, price_override)
			VALUES ($1,$2,$3)
			ON CONFLICT (store_id, product_id) DO UPDATE SET price_override=EXCLUDED.price_override`,
			sid, in.ProductID, in.PriceOverride)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true})
	})

	// Store: remove product
	app.Delete("/v1/stores/:id/products/:pid", func(c *fiber.Ctx) error {
		sid := c.Params("id")
		pid := c.Params("pid")
		uid := auth.UserID(c)
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var allowed bool
		_ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM stores WHERE id=$1 AND owner_user_id=$2)`, sid, uid)
		if !allowed {
			_ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role IN ('platform.admin','admin'))`, uid)
		}
		if !allowed {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false})
		}
		_, err := opts.DB.Exec(`DELETE FROM store_products WHERE store_id=$1 AND product_id=$2`, sid, pid)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true})
	})

	// Admin: manage user roles (platform.admin only)
	app.Get("/v1/admin/users/:id/roles", func(c *fiber.Ctx) error {
		uid := auth.UserID(c)
		target := c.Params("id")
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var isAdmin bool
		_ = opts.DB.Get(&isAdmin, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role='platform.admin')`, uid)
		if !isAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false})
		}
		roles := []struct {
			Role string `db:"role" json:"role"`
		}{}
		if err := opts.DB.Select(&roles, `SELECT role FROM user_roles WHERE user_id=$1 ORDER BY role`, target); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": roles})
	})

	app.Post("/v1/admin/users/:id/roles", func(c *fiber.Ctx) error {
		uid := auth.UserID(c)
		target := c.Params("id")
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var isAdmin bool
		_ = opts.DB.Get(&isAdmin, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role='platform.admin')`, uid)
		if !isAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false})
		}
		var in struct {
			Role string `json:"role"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Role) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		_, err := opts.DB.Exec(`INSERT INTO user_roles (user_id, role) VALUES ($1,$2) ON CONFLICT (user_id, role) DO NOTHING`, target, in.Role)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true})
	})

	app.Delete("/v1/admin/users/:id/roles", func(c *fiber.Ctx) error {
		uid := auth.UserID(c)
		target := c.Params("id")
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var isAdmin bool
		_ = opts.DB.Get(&isAdmin, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role='platform.admin')`, uid)
		if !isAdmin {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false})
		}
		var in struct {
			Role string `json:"role"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Role) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}
		_, err := opts.DB.Exec(`DELETE FROM user_roles WHERE user_id=$1 AND role=$2`, target, in.Role)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true})
	})
}
