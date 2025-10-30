package server

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func registerPublicRoutes(app *fiber.App, opts Options) {
	// Public: search warehouses
	app.Get("/v1/public/warehouses", func(c *fiber.Ctx) error {
		type pubWarehouse struct {
			ID          string   `db:"id" json:"id"`
			Name        string   `db:"name" json:"name"`
			Location    *string  `db:"location" json:"location,omitempty"`
			Lat         *float64 `db:"lat" json:"lat,omitempty"`
			Lng         *float64 `db:"lng" json:"lng,omitempty"`
			Kind        *string  `db:"kind" json:"kind,omitempty"`
			IsMultiUnit bool     `db:"is_multi_unit" json:"isMultiUnit"`
			State       string   `db:"state" json:"state"`
			PriceAmount *float64 `db:"price_amount" json:"priceAmount,omitempty"`
			Currency    *string  `db:"currency" json:"currency,omitempty"`
			PricingMode *string  `db:"pricing_mode" json:"pricingMode,omitempty"`
			AreaSqm     *float64 `db:"area_sqm" json:"areaSqm,omitempty"`
			DistanceKm  *float64 `db:"distance_km" json:"distanceKm,omitempty"`
		}
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		q := c.Query("q")
		near := c.Query("near") // format: lat,lng
		radiusKm := c.Query("radius_km", "50")
		limit := c.Query("limit", "50")
		page := c.Query("page", "1")
		params := []any{}
		selectBase := `SELECT w.id, w.name, w.location, w.lat, w.lng, w.kind, w.is_multi_unit, w.state, w.price_amount, w.currency, w.pricing_mode, w.area_sqm`
		where := ` WHERE 1=1`
		order := ` ORDER BY w.name`
		having := ``
		withDistance := false
		if q != "" {
			where += " AND LOWER(w.name) LIKE LOWER($1)"
			params = append(params, "%"+q+"%")
		}
		// near parsing
		var lat, lng float64
		if near != "" {
			if p := strings.Split(near, ","); len(p) == 2 {
				lat, _ = strconv.ParseFloat(strings.TrimSpace(p[0]), 64)
				lng, _ = strconv.ParseFloat(strings.TrimSpace(p[1]), 64)
				withDistance = true
			}
		}
		if withDistance {
			selectBase += ", (6371 * acos( cos(radians($%d)) * cos(radians(w.lat)) * cos(radians(w.lng) - radians($%d)) + sin(radians($%d)) * sin(radians(w.lat)) )) AS distance_km"
			latIdx := len(params) + 1
			lngIdx := len(params) + 2
			lat2Idx := len(params) + 3
			selectBase = fmt.Sprintf(selectBase, latIdx, lngIdx, lat2Idx)
			params = append(params, lat, lng, lat)
			where += " AND w.lat IS NOT NULL AND w.lng IS NOT NULL"
			rIdx := len(params) + 1
			where += fmt.Sprintf(" AND (6371 * acos( cos(radians($%d)) * cos(radians(w.lat)) * cos(radians(w.lng) - radians($%d)) + sin(radians($%d)) * sin(radians(w.lat)) )) <= $%d", latIdx, lngIdx, lat2Idx, rIdx)
			r, _ := strconv.ParseFloat(radiusKm, 64)
			params = append(params, r)
			order = " ORDER BY distance_km ASC, w.name"
		}
		lim, _ := strconv.Atoi(limit)
		if lim <= 0 || lim > 200 {
			lim = 50
		}
		pg, _ := strconv.Atoi(page)
		if pg <= 0 {
			pg = 1
		}
		offset := (pg - 1) * lim
		limitSQL := fmt.Sprintf(" LIMIT %d OFFSET %d", lim, offset)
		sql := selectBase + " FROM warehouses w " + where + having + order + limitSQL
		rows := []pubWarehouse{}
		if err := opts.DB.Select(&rows, sql, params...); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "search failed"})
		}
		return c.JSON(fiber.Map{"success": true, "data": rows})
	})

	// Public: warehouse detail by ID (limited fields)
	app.Get("/v1/public/warehouses/:id", func(c *fiber.Ctx) error {
		type pubWarehouse struct {
			ID          string   `db:"id" json:"id"`
			Name        string   `db:"name" json:"name"`
			Location    *string  `db:"location" json:"location,omitempty"`
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
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		var row pubWarehouse
		if err := opts.DB.Get(&row, `SELECT id, name, location, lat, lng, kind, is_multi_unit, state, price_amount, currency, pricing_mode, area_sqm FROM warehouses WHERE id=$1`, id); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
		}
		return c.JSON(fiber.Map{"success": true, "data": row})
	})

	// Public: search products (inventory) by name/SKU and optional location
	app.Get("/v1/public/products", func(c *fiber.Ctx) error {
		type pubProduct struct {
			SKU           string   `db:"sku" json:"sku"`
			Name          string   `db:"name" json:"name"`
			Quantity      int      `db:"quantity" json:"quantity"`
			WarehouseID   string   `db:"warehouse_id" json:"warehouseId"`
			WarehouseName string   `db:"warehouse_name" json:"warehouseName"`
			Location      *string  `db:"location" json:"location,omitempty"`
			Lat           *float64 `db:"lat" json:"lat,omitempty"`
			Lng           *float64 `db:"lng" json:"lng,omitempty"`
			DistanceKm    *float64 `db:"distance_km" json:"distanceKm,omitempty"`
		}
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		q := c.Query("q")
		near := c.Query("near") // lat,lng
		radiusKm := c.Query("radius_km", "50")
		limit := c.Query("limit", "50")
		page := c.Query("page", "1")

		params := []any{}
		selectBase := `SELECT i.sku, i.name, i.quantity, i.warehouse_id, w.name AS warehouse_name, w.location, w.lat, w.lng`
		from := ` FROM inventory i JOIN warehouses w ON w.id = i.warehouse_id`
		where := ` WHERE 1=1`
		order := ` ORDER BY i.name`
		withDistance := false

		if q != "" {
			where += " AND (LOWER(i.name) LIKE LOWER($1) OR LOWER(i.sku) LIKE LOWER($1))"
			params = append(params, "%"+q+"%")
		}
		var lat, lng float64
		if near != "" {
			if p := strings.Split(near, ","); len(p) == 2 {
				lat, _ = strconv.ParseFloat(strings.TrimSpace(p[0]), 64)
				lng, _ = strconv.ParseFloat(strings.TrimSpace(p[1]), 64)
				withDistance = true
			}
		}
		if withDistance {
			latIdx := len(params) + 1
			lngIdx := len(params) + 2
			lat2Idx := len(params) + 3
			selectBase += ", (6371 * acos( cos(radians($%d)) * cos(radians(w.lat)) * cos(radians(w.lng) - radians($%d)) + sin(radians($%d)) * sin(radians(w.lat)) )) AS distance_km"
			selectBase = fmt.Sprintf(selectBase, latIdx, lngIdx, lat2Idx)
			params = append(params, lat, lng, lat)
			where += " AND w.lat IS NOT NULL AND w.lng IS NOT NULL"
			rIdx := len(params) + 1
			r, _ := strconv.ParseFloat(radiusKm, 64)
			where += fmt.Sprintf(" AND (6371 * acos( cos(radians($%d)) * cos(radians(w.lat)) * cos(radians(w.lng) - radians($%d)) + sin(radians($%d)) * sin(radians(w.lat)) )) <= $%d", latIdx, lngIdx, lat2Idx, rIdx)
			params = append(params, r)
			order = " ORDER BY distance_km ASC, i.name"
		}
		lim, _ := strconv.Atoi(limit)
		if lim <= 0 || lim > 200 {
			lim = 50
		}
		pg, _ := strconv.Atoi(page)
		if pg <= 0 {
			pg = 1
		}
		offset := (pg - 1) * lim
		limitSQL := fmt.Sprintf(" LIMIT %d OFFSET %d", lim, offset)
		sql := selectBase + from + where + order + limitSQL
		out := []pubProduct{}
		if err := opts.DB.Select(&out, sql, params...); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "search failed"})
		}
		return c.JSON(fiber.Map{"success": true, "data": out})
	})

	// Public: product detail by inventory ID
	app.Get("/v1/public/products/:id", func(c *fiber.Ctx) error {
		type pubProduct struct {
			ID            string   `db:"id" json:"id"`
			SKU           string   `db:"sku" json:"sku"`
			Name          string   `db:"name" json:"name"`
			Quantity      int      `db:"quantity" json:"quantity"`
			WarehouseID   string   `db:"warehouse_id" json:"warehouseId"`
			WarehouseName string   `db:"warehouse_name" json:"warehouseName"`
			Location      *string  `db:"location" json:"location,omitempty"`
			Lat           *float64 `db:"lat" json:"lat,omitempty"`
			Lng           *float64 `db:"lng" json:"lng,omitempty"`
		}
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		var row pubProduct
		if err := opts.DB.Get(&row, `SELECT i.id, i.sku, i.name, i.quantity, i.warehouse_id, w.name AS warehouse_name, w.location, w.lat, w.lng
          FROM inventory i JOIN warehouses w ON w.id=i.warehouse_id WHERE i.id=$1`, id); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
		}
		return c.JSON(fiber.Map{"success": true, "data": row})
	})

	// Public: store landing by slug
	app.Get("/v1/public/store/:slug", func(c *fiber.Ctx) error {
		slug := c.Params("slug")
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		var store struct {
			ID          string  `db:"id" json:"id"`
			Name        string  `db:"name" json:"name"`
			Slug        string  `db:"slug" json:"slug"`
			Description *string `db:"description" json:"description"`
		}
		if err := opts.DB.Get(&store, `SELECT id, name, slug, description FROM stores WHERE LOWER(slug)=LOWER($1) AND status='active'`, slug); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		products := []struct {
			ID            string   `db:"id" json:"id"`
			Name          string   `db:"name" json:"name"`
			SKU           *string  `db:"sku" json:"sku"`
			Price         *float64 `db:"price" json:"price"`
			Currency      *string  `db:"currency" json:"currency"`
			PriceOverride *float64 `db:"price_override" json:"priceOverride"`
		}{}
		_ = opts.DB.Select(&products, `SELECT p.id, p.name, p.sku, p.price, p.currency, sp.price_override FROM store_products sp JOIN products p ON p.id = sp.product_id WHERE sp.store_id=$1 ORDER BY p.name`, store.ID)
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"store": store, "products": products}})
	})

	// Public: search transport (vehicles) with minimal fields, optional proximity
	app.Get("/v1/public/transport", func(c *fiber.Ctx) error {
		type pubVehicle struct {
			ID         string   `db:"id" json:"id"`
			Kind       *string  `db:"kind" json:"kind,omitempty"`
			CapacityKg *float64 `db:"capacity_kg" json:"capacityKg,omitempty"`
			Lat        *float64 `db:"lat" json:"lat,omitempty"`
			Lng        *float64 `db:"lng" json:"lng,omitempty"`
			DistanceKm *float64 `db:"distance_km" json:"distanceKm,omitempty"`
		}
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		q := c.Query("q") // filter by kind
		minCap := c.Query("min_capacity_kg")
		near := c.Query("near")
		radiusKm := c.Query("radius_km", "50")
		limit := c.Query("limit", "50")
		page := c.Query("page", "1")
		params := []any{}
		selectBase := "SELECT id, kind, capacity_kg, lat, lng"
		where := " WHERE 1=1"
		order := " ORDER BY kind NULLS LAST, capacity_kg DESC NULLS LAST"
		withDistance := false
		if q != "" {
			where += " AND LOWER(kind) LIKE LOWER($1)"
			params = append(params, "%"+q+"%")
		}
		if minCap != "" {
			if _, err := strconv.ParseFloat(minCap, 64); err == nil {
				idx := len(params) + 1
				where += fmt.Sprintf(" AND capacity_kg >= $%d", idx)
				params = append(params, minCap)
			}
		}
		var lat, lng float64
		if near != "" {
			if p := strings.Split(near, ","); len(p) == 2 {
				lat, _ = strconv.ParseFloat(strings.TrimSpace(p[0]), 64)
				lng, _ = strconv.ParseFloat(strings.TrimSpace(p[1]), 64)
				withDistance = true
			}
		}
		if withDistance {
			latIdx := len(params) + 1
			lngIdx := len(params) + 2
			lat2Idx := len(params) + 3
			selectBase += ", (6371 * acos( cos(radians($%d)) * cos(radians(lat)) * cos(radians(lng) - radians($%d)) + sin(radians($%d)) * sin(radians(lat)) )) AS distance_km"
			selectBase = fmt.Sprintf(selectBase, latIdx, lngIdx, lat2Idx)
			params = append(params, lat, lng, lat)
			where += " AND lat IS NOT NULL AND lng IS NOT NULL"
			rIdx := len(params) + 1
			r, _ := strconv.ParseFloat(radiusKm, 64)
			where += fmt.Sprintf(" AND (6371 * acos( cos(radians($%d)) * cos(radians(lat)) * cos(radians(lng) - radians($%d)) + sin(radians($%d)) * sin(radians(lat)) )) <= $%d", latIdx, lngIdx, lat2Idx, rIdx)
			params = append(params, r)
			order = " ORDER BY distance_km ASC, kind NULLS LAST, capacity_kg DESC NULLS LAST"
		}
		lim, _ := strconv.Atoi(limit)
		if lim <= 0 || lim > 200 {
			lim = 50
		}
		pg, _ := strconv.Atoi(page)
		if pg <= 0 {
			pg = 1
		}
		offset := (pg - 1) * lim
		sql := fmt.Sprintf("%s FROM vehicles%s%s LIMIT %d OFFSET %d", selectBase, where, order, lim, offset)
		out := []pubVehicle{}
		if err := opts.DB.Select(&out, sql, params...); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "search failed"})
		}
		return c.JSON(fiber.Map{"success": true, "data": out})
	})
}
