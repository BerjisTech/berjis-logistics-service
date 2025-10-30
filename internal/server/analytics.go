package server

import (
	"math"

	"github.com/gofiber/fiber/v2"

	"github.com/berjistech/berjis-ecosystem/logistics/service/internal/auth"
)

func registerAnalyticsRoutes(app *fiber.App, opts Options) {
	app.Get("/v1/analytics/dashboard", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}

		metrics := dashboardAnalyticsRow{}

		// Storage utilisation
		var storage struct {
			Reserved       float64 `db:"reserved"`
			Capacity       float64 `db:"capacity"`
			WarehouseCount int     `db:"warehouses"`
			ActiveBookings int     `db:"active_bookings"`
		}
		if err := opts.DB.Get(&storage, `
			SELECT
				COALESCE(SUM(b.space_reserved), 0) AS reserved,
				COALESCE(SUM(COALESCE(w.area_sqm, 0)), 0) AS capacity,
				COUNT(DISTINCT w.id) AS warehouses,
				COUNT(DISTINCT CASE WHEN b.id IS NOT NULL THEN b.id END) AS active_bookings
			FROM warehouses w
			LEFT JOIN bookings b ON b.warehouse_id = w.id AND b.status NOT IN ('cancelled')
			WHERE w.owner_user_id = $1`, uid); err == nil {
			if storage.Capacity > 0 {
				metrics.StorageUtilisation = math.Min(100, (storage.Reserved/storage.Capacity)*100)
			} else if storage.WarehouseCount > 0 {
				metrics.StorageUtilisation = math.Min(100, (float64(storage.ActiveBookings)/float64(storage.WarehouseCount))*100)
			}
		}

		// OTIF (completed deliveries vs total)
		var otif struct {
			Completed float64 `db:"completed"`
			Total     float64 `db:"total"`
		}
		if err := opts.DB.Get(&otif, `
			SELECT
				SUM(CASE WHEN d.status = 'completed' THEN 1 ELSE 0 END) AS completed,
				SUM(1) AS total
			FROM deliveries d
			LEFT JOIN vehicles v ON v.id = d.vehicle_id
			LEFT JOIN drivers dr ON dr.id = d.driver_id
			WHERE v.owner_user_id = $1 OR dr.user_id = $1`, uid); err == nil && otif.Total > 0 {
			metrics.OtifPercentage = math.Min(100, (otif.Completed/otif.Total)*100)
		} else {
			metrics.OtifPercentage = 100
		}

		// Marketplace velocity (products in last 7 days)
		var products struct {
			Count int `db:"count"`
		}
		if err := opts.DB.Get(&products, `SELECT COUNT(*) AS count FROM products WHERE owner_user_id=$1 AND created_at >= NOW() - INTERVAL '7 days'`, uid); err == nil {
			metrics.MarketplaceVelocity = products.Count
		}

		// Fleet availability
		var fleet struct {
			Count int `db:"count"`
		}
		if err := opts.DB.Get(&fleet, `
			SELECT COUNT(*) AS count
			FROM vehicles v
			WHERE v.owner_user_id = $1
			  AND NOT EXISTS (
				SELECT 1 FROM deliveries d
				WHERE d.vehicle_id = v.id AND d.status <> 'completed'
			)`, uid); err == nil {
			metrics.FleetAvailable = fleet.Count
		}

		// Invoice dues
		var invoices struct {
			Due float64 `db:"due"`
		}
		if err := opts.DB.Get(&invoices, `SELECT COALESCE(SUM(amount_due), 0) AS due FROM invoices WHERE owner_user_id=$1 AND status <> 'void'`, uid); err == nil {
			metrics.InvoiceDue = invoices.Due
		}

		// Marketing campaigns running
		var campaigns struct {
			Count int `db:"count"`
		}
		if err := opts.DB.Get(&campaigns, `SELECT COUNT(*) AS count FROM marketing_campaigns WHERE owner_user_id=$1 AND status='running'`, uid); err == nil {
			metrics.CampaignsRunning = campaigns.Count
		}

		// Shipments stats (reuse)
		stats, err := computeShipmentStats(opts, uid)
		if err == nil {
			metrics.ShipmentsActive = stats.Active
			metrics.ShipmentsCompleted24h = stats.Completed24h
		}

		return c.JSON(fiber.Map{"success": true, "data": metrics})
	})

	app.Get("/v1/analytics/shipments", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		stats, err := computeShipmentStats(opts, uid)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": stats})
	})
}

type dashboardAnalyticsRow struct {
	StorageUtilisation    float64 `json:"storageUtilisation"`
	OtifPercentage        float64 `json:"otifPercentage"`
	MarketplaceVelocity   int     `json:"marketplaceVelocity"`
	FleetAvailable        int     `json:"fleetAvailable"`
	InvoiceDue            float64 `json:"invoiceDue"`
	CampaignsRunning      int     `json:"campaignsRunning"`
	ShipmentsActive       int     `json:"shipmentsActive"`
	ShipmentsCompleted24h int     `json:"shipmentsCompleted24h"`
}

type shipmentsAnalyticsRow struct {
	Active       int `json:"active"`
	Running      int `json:"running"`
	Delayed      int `json:"delayed"`
	Completed24h int `json:"completed24h"`
}

func computeShipmentStats(opts Options, uid string) (shipmentsAnalyticsRow, error) {
	var stats shipmentsAnalyticsRow
	if opts.DB == nil || uid == "" {
		return stats, fiber.ErrInternalServerError
	}
	var row struct {
		Active       int `db:"active"`
		Running      int `db:"running"`
		Delayed      int `db:"delayed"`
		Completed24h int `db:"completed_24h"`
	}
	err := opts.DB.Get(&row, `
		SELECT
			COALESCE(SUM(CASE WHEN d.status <> 'completed' THEN 1 ELSE 0 END),0) AS active,
			COALESCE(SUM(CASE WHEN d.status <> 'completed' AND d.updated_at >= NOW() - INTERVAL '6 hours' THEN 1 ELSE 0 END),0) AS running,
			COALESCE(SUM(CASE WHEN d.status <> 'completed' AND d.updated_at < NOW() - INTERVAL '1 day' THEN 1 ELSE 0 END),0) AS delayed,
			COALESCE(SUM(CASE WHEN d.status = 'completed' AND d.updated_at >= NOW() - INTERVAL '24 hours' THEN 1 ELSE 0 END),0) AS completed_24h
		FROM deliveries d
		LEFT JOIN vehicles v ON v.id = d.vehicle_id
		LEFT JOIN drivers dr ON dr.id = d.driver_id
		WHERE v.owner_user_id = $1 OR dr.user_id = $1`, uid)
	if err != nil {
		return stats, err
	}
	stats.Active = row.Active
	stats.Running = row.Running
	stats.Delayed = row.Delayed
	stats.Completed24h = row.Completed24h
	return stats, nil
}
