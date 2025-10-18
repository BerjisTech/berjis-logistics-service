package server

import (
    "fmt"
    "crypto/rand"
    "encoding/hex"
    "strconv"
    "strings"
    "time"

    "github.com/jmoiron/sqlx"
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/cors"
    "github.com/gofiber/fiber/v2/middleware/limiter"
    "github.com/berjistech/berjis-ecosystem/logistics/service/internal/auth"
    websocketlib "github.com/gofiber/websocket/v2"
    "golang.org/x/crypto/bcrypt"
)

type Options struct {
    AllowedOrigins string
    DB             *sqlx.DB
    Env            string
    AuthHS256      string
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

type Booking struct {
    ID            string  `db:"id" json:"id"`
    WarehouseID   string  `db:"warehouse_id" json:"warehouseId"`
    TenantUserID  string  `db:"tenant_user_id" json:"tenantUserId"`
    SpaceReserved *float64 `db:"space_reserved" json:"spaceReserved,omitempty"`
    StartDate     string  `db:"start_date" json:"startDate"`
    EndDate       *string `db:"end_date" json:"endDate,omitempty"`
    PricePerDay   *float64 `db:"price_per_day" json:"pricePerDay,omitempty"`
    Status        string  `db:"status" json:"status"`
}

// WebSocket subscribers for live positions
type positionSub struct {
    conn *websocketlib.Conn
    vehicleID string
    assetID string
}

var wsSubs = struct { list []*positionSub }{ list: []*positionSub{} }

func broadcastPosition(kind string, id string, lat, lng float64, ts time.Time) {
    for i := 0; i < len(wsSubs.list); i++ {
        sub := wsSubs.list[i]
        if kind == "vehicle" && sub.vehicleID != "" && sub.vehicleID != id { continue }
        if kind == "asset" && sub.assetID != "" && sub.assetID != id { continue }
        msg := fmt.Sprintf(`{"kind":"%s","id":"%s","lat":%f,"lng":%f,"ts":"%s"}`, kind, id, lat, lng, ts.UTC().Format(time.RFC3339Nano))
        if err := sub.conn.WriteMessage(websocketlib.TextMessage, []byte(msg)); err != nil {
            _ = sub.conn.Close()
            wsSubs.list = append(wsSubs.list[:i], wsSubs.list[i+1:]...)
            i--
        }
    }
}

func broadcastJSON(msg string) {
    for i := 0; i < len(wsSubs.list); i++ {
        sub := wsSubs.list[i]
        if err := sub.conn.WriteMessage(websocketlib.TextMessage, []byte(msg)); err != nil {
            _ = sub.conn.Close()
            wsSubs.list = append(wsSubs.list[:i], wsSubs.list[i+1:]...)
            i--
        }
    }
}

// Helpers for permissions
func isOwnerOrAdmin(db *sqlx.DB, warehouseID, userID string) bool {
    var ok bool
    _ = db.Get(&ok, `SELECT EXISTS (
        SELECT 1 FROM warehouses w WHERE w.id=$1 AND w.owner_user_id=$2
      ) OR EXISTS (
        SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=$1 AND s.user_id=$2 AND s.role IN ('admin')
      )`, warehouseID, userID)
    return ok
}

func hasPermission(db *sqlx.DB, warehouseID, userID, perm string) bool {
    var ok bool
    _ = db.Get(&ok, `SELECT EXISTS (
       SELECT 1 FROM warehouses w WHERE w.id=$1 AND w.owner_user_id=$2
     ) OR EXISTS (
       SELECT 1 FROM warehouse_staff s
       WHERE s.warehouse_id=$1 AND s.user_id=$2 AND (
         s.role IN ('admin') OR (s.permissions ->> $3) = 'true'
       )
     )`, warehouseID, userID, perm)
    return ok
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

    // Rate limit public discovery endpoints
    app.Use("/v1/public/*", limiter.New(limiter.Config{ Max: 100, Expiration: 1 * time.Minute }))
    // Rate limit device ingest endpoints
    app.Use("/v1/ingest/*", limiter.New(limiter.Config{ Max: 300, Expiration: 1 * time.Minute }))

    // Public: search warehouses by location/name (no auth required)
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
            PriceUnit   *string  `db:"price_unit" json:"priceUnit,omitempty"`
            PricingMode *string  `db:"pricing_mode" json:"pricingMode,omitempty"`
            AreaSqm     *float64 `db:"area_sqm" json:"areaSqm,omitempty"`
            DistanceKm  *float64 `db:"distance_km" json:"distanceKm,omitempty"`
        }
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        q := c.Query("q")
        near := c.Query("near") // format: lat,lng
        radiusKm := c.Query("radius_km", "50")
        limit := c.Query("limit", "50")
        page := c.Query("page", "1")
        // Parse paging
        // simple conversion with defaults
        // Build SQL
        params := []any{}
        selectBase := `SELECT w.id, w.name, w.location, w.lat, w.lng, w.kind, w.is_multi_unit, w.state, w.price_amount, w.price_unit, w.pricing_mode, w.area_sqm`
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
            // parse
            if p := strings.Split(near, ","); len(p) == 2 {
                lat, _ = strconv.ParseFloat(strings.TrimSpace(p[0]), 64)
                lng, _ = strconv.ParseFloat(strings.TrimSpace(p[1]), 64)
                withDistance = true
            }
        }
        if withDistance {
            // shift param indexes accordingly
            selectBase += ", (6371 * acos( cos(radians($%d)) * cos(radians(w.lat)) * cos(radians(w.lng) - radians($%d)) + sin(radians($%d)) * sin(radians(w.lat)) )) AS distance_km"
            // Compute positional indexes for lat/lng/lat depending on existing params
            latIdx := len(params) + 1
            lngIdx := len(params) + 2
            lat2Idx := len(params) + 3
            selectBase = fmt.Sprintf(selectBase, latIdx, lngIdx, lat2Idx)
            params = append(params, lat, lng, lat)
            where += " AND w.lat IS NOT NULL AND w.lng IS NOT NULL"
            // radius filter
            rIdx := len(params) + 1
            where += fmt.Sprintf(" AND (6371 * acos( cos(radians($%d)) * cos(radians(w.lat)) * cos(radians(w.lng) - radians($%d)) + sin(radians($%d)) * sin(radians(w.lat)) )) <= $%d", latIdx, lngIdx, lat2Idx, rIdx)
            // parse radius
            r, _ := strconv.ParseFloat(radiusKm, 64)
            params = append(params, r)
            order = " ORDER BY distance_km ASC, w.name"
        }
        // Pagination
        lim, _ := strconv.Atoi(limit)
        if lim <= 0 || lim > 200 { lim = 50 }
        pg, _ := strconv.Atoi(page)
        if pg <= 0 { pg = 1 }
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
            PriceUnit   *string  `db:"price_unit" json:"priceUnit,omitempty"`
            PricingMode *string  `db:"pricing_mode" json:"pricingMode,omitempty"`
            AreaSqm     *float64 `db:"area_sqm" json:"areaSqm,omitempty"`
        }
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        id := c.Params("id")
        var row pubWarehouse
        if err := opts.DB.Get(&row, `SELECT id, name, location, lat, lng, kind, is_multi_unit, state, price_amount, price_unit, pricing_mode, area_sqm FROM warehouses WHERE id=$1`, id); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
        }
        return c.JSON(fiber.Map{"success": true, "data": row})
    })

    // Public: search products (inventory) by name/SKU and optional location
    app.Get("/v1/public/products", func(c *fiber.Ctx) error {
        type pubProduct struct {
            SKU        string   `db:"sku" json:"sku"`
            Name       string   `db:"name" json:"name"`
            Quantity   int      `db:"quantity" json:"quantity"`
            WarehouseID string  `db:"warehouse_id" json:"warehouseId"`
            WarehouseName string `db:"warehouse_name" json:"warehouseName"`
            Location   *string  `db:"location" json:"location,omitempty"`
            Lat        *float64 `db:"lat" json:"lat,omitempty"`
            Lng        *float64 `db:"lng" json:"lng,omitempty"`
            DistanceKm *float64 `db:"distance_km" json:"distanceKm,omitempty"`
        }
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
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
        if lim <= 0 || lim > 200 { lim = 50 }
        pg, _ := strconv.Atoi(page)
        if pg <= 0 { pg = 1 }
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
            ID          string   `db:"id" json:"id"`
            SKU         string   `db:"sku" json:"sku"`
            Name        string   `db:"name" json:"name"`
            Quantity    int      `db:"quantity" json:"quantity"`
            WarehouseID string   `db:"warehouse_id" json:"warehouseId"`
            WarehouseName string `db:"warehouse_name" json:"warehouseName"`
            Location    *string  `db:"location" json:"location,omitempty"`
            Lat         *float64 `db:"lat" json:"lat,omitempty"`
            Lng         *float64 `db:"lng" json:"lng,omitempty"`
        }
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        id := c.Params("id")
        var row pubProduct
        if err := opts.DB.Get(&row, `SELECT i.id, i.sku, i.name, i.quantity, i.warehouse_id, w.name AS warehouse_name, w.location, w.lat, w.lng
          FROM inventory i JOIN warehouses w ON w.id=i.warehouse_id WHERE i.id=$1`, id); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
        }
        return c.JSON(fiber.Map{"success": true, "data": row})
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
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
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
        if lim <= 0 || lim > 200 { lim = 50 }
        pg, _ := strconv.Atoi(page)
        if pg <= 0 { pg = 1 }
        offset := (pg - 1) * lim
        sql := fmt.Sprintf("%s FROM vehicles%s%s LIMIT %d OFFSET %d", selectBase, where, order, lim, offset)
        out := []pubVehicle{}
        if err := opts.DB.Select(&out, sql, params...); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "search failed"})
        }
        return c.JSON(fiber.Map{"success": true, "data": out})
    })

    // WebSocket: live positions (requires auth by placement after auth middleware below)
    app.Use("/v1/ws", func(c *fiber.Ctx) error {
        if websocketlib.IsWebSocketUpgrade(c) { return c.Next() }
        return fiber.ErrUpgradeRequired
    })

    // Protect all following routes
    app.Use(auth.Middleware(auth.Options{HS256Secret: opts.AuthHS256, Env: opts.Env}))

    app.Get("/v1/ws/positions", websocketlib.New(func(conn *websocketlib.Conn) {
        vehicleID := conn.Query("vehicleId")
        assetID := conn.Query("assetId")
        sub := &positionSub{conn: conn, vehicleID: vehicleID, assetID: assetID}
        wsSubs.list = append(wsSubs.list, sub)
        defer func() {
            for i, s := range wsSubs.list { if s == sub { wsSubs.list = append(wsSubs.list[:i], wsSubs.list[i+1:]...); break } }
            _ = conn.Close()
        }()
        for {
            if _, _, err := conn.ReadMessage(); err != nil { break }
        }
    }))

    // Warehouses list
    app.Get("/v1/warehouses", func(c *fiber.Ctx) error {
        rows := []Warehouse{}
        if opts.DB != nil {
            uid := auth.UserID(c)
            if err := opts.DB.Select(&rows, `SELECT w.id, w.name, COALESCE(w.location, '') AS location,
                w.owner_user_id, w.lat, w.lng, w.kind, w.is_multi_unit, w.state, w.price_amount, w.price_unit, w.pricing_mode, w.area_sqm
              FROM warehouses w
              WHERE w.owner_user_id = $1 OR EXISTS (
                SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id = w.id AND s.user_id = $1
              )
              ORDER BY w.name`, uid); err != nil {
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
        uid := auth.UserID(c)
        if err := opts.DB.Get(&w, `SELECT id, name, COALESCE(location, '') AS location,
            owner_user_id, lat, lng, kind, is_multi_unit, state, price_amount, price_unit, pricing_mode, area_sqm
          FROM warehouses WHERE id=$1 AND (owner_user_id=$2 OR EXISTS (
            SELECT 1 FROM warehouse_staff WHERE warehouse_id=$1 AND user_id=$2
          ))`, id, uid); err != nil {
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

    // Warehouse create
    app.Post("/v1/warehouses", func(c *fiber.Ctx) error {
        userID := auth.UserID(c)
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

    // Warehouse update (restrict edits by permission)
    app.Put("/v1/warehouses/:id", func(c *fiber.Ctx) error {
        id := c.Params("id")
        uid := auth.UserID(c)
        var in warehouseIn
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        if in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "name required"}) }
        var w Warehouse
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        // Permission gates
        priceChange := (in.PriceAmount != nil) || (in.PriceUnit != nil) || (in.PricingMode != nil)
        stateChange := (in.State != nil)
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
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        res, err := opts.DB.Exec(`DELETE FROM warehouses WHERE id=$1 AND (
            owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff WHERE warehouse_id=$1 AND user_id=$2 AND role IN ('admin')
            )
          )`, id, uid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        n, _ := res.RowsAffected()
        if n == 0 { return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"}) }
        return c.JSON(fiber.Map{"success": true})
    })

    // Inventory: list by warehouse (must have access)
    app.Get("/v1/warehouses/:id/inventory", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
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

    // Staff: list (owner/admin or manage_staff)
    app.Get("/v1/warehouses/:id/staff", func(c *fiber.Ctx) error {
        type staffRow struct { UserID string `db:"user_id" json:"userId"`; Role string `db:"role" json:"role"`; Permissions map[string]any `db:"permissions" json:"permissions"` }
        wid := c.Params("id")
        uid := auth.UserID(c)
        out := []staffRow{}
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if !(isOwnerOrAdmin(opts.DB, wid, uid) || hasPermission(opts.DB, wid, uid, "manage_staff")) {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        if err := opts.DB.Select(&out, `SELECT user_id, role, COALESCE(permissions,'{}'::jsonb) AS permissions FROM warehouse_staff WHERE warehouse_id=$1 ORDER BY role`, wid); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": out})
    })

    // Staff: add/update role and permissions (owner or admin with manage_staff)
    app.Post("/v1/warehouses/:id/staff", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        uid := auth.UserID(c)
        var in struct { UserID string `json:"userId"`; Role string `json:"role"`; Permissions map[string]any `json:"permissions"` }
        if err := c.BodyParser(&in); err != nil || in.UserID == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if !(isOwnerOrAdmin(opts.DB, wid, uid) || hasPermission(opts.DB, wid, uid, "manage_staff")) {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        _, err := opts.DB.Exec(`INSERT INTO warehouse_staff (warehouse_id, user_id, role, permissions)
          VALUES ($1,$2,COALESCE(NULLIF($3,''),'staff'), COALESCE($4,'{}'::jsonb))
          ON CONFLICT (warehouse_id, user_id) DO UPDATE SET role=EXCLUDED.role, permissions=COALESCE(EXCLUDED.permissions,'{}'::jsonb)`, wid, in.UserID, in.Role, in.Permissions)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })

    // Staff: remove (owner or admin with manage_staff)
    app.Delete("/v1/warehouses/:id/staff/:user", func(c *fiber.Ctx) error {
        wid := c.Params("id"); member := c.Params("user")
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        if !(isOwnerOrAdmin(opts.DB, wid, uid) || hasPermission(opts.DB, wid, uid, "manage_staff")) {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        _, err := opts.DB.Exec(`DELETE FROM warehouse_staff WHERE warehouse_id=$1 AND user_id=$2`, wid, member)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })

    // Units: list (must have access)
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
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var allowed bool
        if err := opts.DB.Get(&allowed, `SELECT EXISTS (
            SELECT 1 FROM warehouses w WHERE w.id=$1 AND (w.owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=w.id AND s.user_id=$2
            ))
          )`, wid, uid); err != nil || !allowed {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        if err := opts.DB.Select(&out, `SELECT id, name, area_sqm, kind, state, price_amount, price_unit, pricing_mode FROM warehouse_units WHERE warehouse_id=$1 ORDER BY name`, wid); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": out})
    })

    // Units: create (admin/owner or manage_units)
    app.Post("/v1/warehouses/:id/units", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        uid := auth.UserID(c)
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
        if !(isOwnerOrAdmin(opts.DB, wid, uid) || hasPermission(opts.DB, wid, uid, "manage_units")) {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        if err := opts.DB.Get(&u, `INSERT INTO warehouse_units (warehouse_id, name, area_sqm, kind, state, price_amount, price_unit, pricing_mode)
           VALUES ($1,$2,$3,$4,COALESCE($5,'available'),$6,$7,$8)
           RETURNING id, name, area_sqm, kind, state, price_amount, price_unit, pricing_mode`, wid, in.Name, in.AreaSqm, in.Kind, in.State, in.PriceAmount, in.PriceUnit, in.PricingMode); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": u})
    })

    // Units: update (admin/owner or manage_units)
    app.Put("/v1/warehouses/:id/units/:unit", func(c *fiber.Ctx) error {
        wid := c.Params("id"); uid := c.Params("unit")
        user := auth.UserID(c)
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
          WHERE id=$8 AND warehouse_id=$9 AND (
            EXISTS (SELECT 1 FROM warehouses w WHERE w.id=$9 AND (w.owner_user_id=$10 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=$9 AND s.user_id=$10 AND (s.role IN ('admin') OR (s.permissions->>'manage_units')='true')
            )))
          )
          RETURNING id, name, area_sqm, kind, state, price_amount, price_unit, pricing_mode`, in.Name, in.AreaSqm, in.Kind, in.State, in.PriceAmount, in.PriceUnit, in.PricingMode, uid, wid, user); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": u})
    })

    // Units: delete (admin/owner or manage_units)
    app.Delete("/v1/warehouses/:id/units/:unit", func(c *fiber.Ctx) error {
        wid := c.Params("id"); uid := c.Params("unit")
        user := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        res, err := opts.DB.Exec(`DELETE FROM warehouse_units WHERE id=$1 AND warehouse_id=$2 AND (
            EXISTS (SELECT 1 FROM warehouses w WHERE w.id=$2 AND (w.owner_user_id=$3 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=$2 AND s.user_id=$3 AND (s.role IN ('admin') OR (s.permissions->>'manage_units')='true')
            )))
          )`, uid, wid, user)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        n, _ := res.RowsAffected(); if n == 0 { return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })

    type inventoryIn struct { SKU string `json:"sku"`; Name string `json:"name"`; Quantity int `json:"quantity"` }

    // Inventory: create item (admin/owner or manage_inventory)
    app.Post("/v1/warehouses/:id/inventory", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        user := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in inventoryIn
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        if in.SKU == "" || in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sku and name required"}) }
        if in.Quantity < 0 { in.Quantity = 0 }
        var it InventoryItem
        if !(isOwnerOrAdmin(opts.DB, wid, user) || hasPermission(opts.DB, wid, user, "manage_inventory")) {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        if err := opts.DB.Get(&it, `INSERT INTO inventory (warehouse_id, sku, name, quantity) VALUES ($1,$2,$3,$4) RETURNING id, warehouse_id, sku, name, quantity`, wid, in.SKU, in.Name, in.Quantity); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "create failed"})
        }
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": it})
    })

    // Inventory: update item (admin/owner or manage_inventory)
    app.Put("/v1/warehouses/:id/inventory/:item", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        iid := c.Params("item")
        user := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in inventoryIn
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        if in.SKU == "" || in.Name == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "sku and name required"}) }
        if in.Quantity < 0 { in.Quantity = 0 }
        var it InventoryItem
        if err := opts.DB.Get(&it, `UPDATE inventory SET sku=$1, name=$2, quantity=$3, updated_at=now()
          WHERE id=$4 AND warehouse_id=$5 AND (
            EXISTS (SELECT 1 FROM warehouses w WHERE w.id=$5 AND (w.owner_user_id=$6 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=$5 AND s.user_id=$6 AND (s.role IN ('admin') OR (s.permissions->>'manage_inventory')='true')
            )))
          )
          RETURNING id, warehouse_id, sku, name, quantity`, in.SKU, in.Name, in.Quantity, iid, wid, user); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"})
        }
        return c.JSON(fiber.Map{"success": true, "data": it})
    })

    // Inventory: delete item (admin/owner or manage_inventory)
    app.Delete("/v1/warehouses/:id/inventory/:item", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        iid := c.Params("item")
        user := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        res, err := opts.DB.Exec(`DELETE FROM inventory WHERE id=$1 AND warehouse_id=$2 AND (
            EXISTS (SELECT 1 FROM warehouses w WHERE w.id=$2 AND (w.owner_user_id=$3 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=$2 AND s.user_id=$3 AND (s.role IN ('admin') OR (s.permissions->>'manage_inventory')='true')
            )))
          )`, iid, wid, user)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        n, _ := res.RowsAffected()
        if n == 0 { return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false, "message": "not found"}) }
        return c.JSON(fiber.Map{"success": true})
    })

    // Warehouse update: restrict price/state edits by permissions
    // Replace the previous update handler block to add permission checks

    // Bookings: create for a warehouse (authenticated user)
    app.Post("/v1/warehouses/:id/bookings", func(c *fiber.Ctx) error {
        wid := c.Params("id")
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in struct {
            StartDate string   `json:"startDate"`
            EndDate   *string  `json:"endDate"`
            SpaceReserved *float64 `json:"spaceReserved"`
        }
        if err := c.BodyParser(&in); err != nil || in.StartDate == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        // Ensure warehouse exists and user has at least visibility (owner/staff). For first pass, restrict to accessible warehouses.
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
          RETURNING id, warehouse_id, tenant_user_id, space_reserved, start_date, end_date, price_per_day, status`, wid, uid, in.SpaceReserved, in.StartDate, in.EndDate); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        // broadcast booking created
        broadcastJSON(fmt.Sprintf(`{"kind":"booking","event":"created","id":"%s","warehouseId":"%s","tenantUserId":"%s","startDate":"%s"}`, b.ID, b.WarehouseID, b.TenantUserID, b.StartDate))
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": b})
    })

    // Bookings: list mine
    app.Get("/v1/bookings", func(c *fiber.Ctx) error {
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        out := []Booking{}
        if err := opts.DB.Select(&out, `SELECT id, warehouse_id, tenant_user_id, space_reserved, start_date, end_date, price_per_day, status
          FROM bookings WHERE tenant_user_id=$1 ORDER BY start_date DESC`, uid); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": out})
    })

    // Bookings: list for a warehouse (owner/admin only)
    app.Get("/v1/warehouses/:id/bookings", func(c *fiber.Ctx) error {
        wid := c.Params("id"); uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var allowed bool
        if err := opts.DB.Get(&allowed, `SELECT EXISTS (
            SELECT 1 FROM warehouses w WHERE w.id=$1 AND (w.owner_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=w.id AND s.user_id=$2 AND s.role IN ('admin')
            ))
          )`, wid, uid); err != nil || !allowed {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        out := []Booking{}
        if err := opts.DB.Select(&out, `SELECT id, warehouse_id, tenant_user_id, space_reserved, start_date, end_date, price_per_day, status FROM bookings WHERE warehouse_id=$1 ORDER BY start_date DESC`, wid); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": out})
    })

    // Bookings: cancel (by tenant or owner/admin)
    app.Delete("/v1/bookings/:id", func(c *fiber.Ctx) error {
        bid := c.Params("id"); uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        res, err := opts.DB.Exec(`UPDATE bookings b SET status='cancelled', updated_at=now()
          WHERE b.id=$1 AND (
            b.tenant_user_id=$2 OR EXISTS (
              SELECT 1 FROM warehouses w WHERE w.id=b.warehouse_id AND (w.owner_user_id=$2 OR EXISTS (
                SELECT 1 FROM warehouse_staff s WHERE s.warehouse_id=w.id AND s.user_id=$2 AND s.role IN ('admin')
              ))
            )
          )`, bid, uid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        n, _ := res.RowsAffected(); if n == 0 { return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false}) }
        broadcastJSON(fmt.Sprintf(`{"kind":"booking","event":"cancelled","id":"%s"}`, bid))
        return c.JSON(fiber.Map{"success": true})
    })

    // Driver/Owner check-in for vehicle tracking
    app.Post("/v1/vehicles/:id/track", func(c *fiber.Ctx) error {
        vid := c.Params("id")
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in struct { Lat float64 `json:"lat"`; Lng float64 `json:"lng"` }
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"}) }
        var allowed bool
        _ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM vehicles v WHERE v.id=$1 AND v.owner_user_id=$2)`, vid, uid)
        if !allowed {
            _ = opts.DB.Get(&allowed, `SELECT EXISTS (
              SELECT 1 FROM drivers d
              JOIN deliveries dv ON dv.driver_id = d.id AND dv.vehicle_id = $1 AND dv.status <> 'completed'
              WHERE d.user_id = $2
            )`, vid, uid)
        }
        if !allowed { return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false}) }
        _, err := opts.DB.Exec(`UPDATE vehicles SET lat=$1, lng=$2, last_seen=now(), updated_at=now() WHERE id=$3`, in.Lat, in.Lng, vid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        broadcastPosition("vehicle", vid, in.Lat, in.Lng, time.Now().UTC())
        return c.JSON(fiber.Map{"success": true})
    })

    // Assets CRUD + check-ins
    app.Post("/v1/assets", func(c *fiber.Ctx) error {
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in struct { Kind string `json:"kind"`; Name *string `json:"name"`; Identifier *string `json:"identifier"` }
        if err := c.BodyParser(&in); err != nil || in.Kind == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        var out struct { ID string `db:"id" json:"id"`; Kind string `db:"kind" json:"kind"`; Name *string `db:"name" json:"name"`; Identifier *string `db:"identifier" json:"identifier"` }
        if err := opts.DB.Get(&out, `INSERT INTO assets (owner_user_id, kind, name, identifier) VALUES ($1,$2,$3,$4) RETURNING id, kind, name, identifier`, uid, in.Kind, in.Name, in.Identifier); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": out})
    })

    // Recent positions
    app.Get("/v1/vehicles/:id/positions", func(c *fiber.Ctx) error {
        vid := c.Params("id"); uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        // owner or active assigned driver
        var allowed bool
        _ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM vehicles WHERE id=$1 AND owner_user_id=$2)`, vid, uid)
        if !allowed {
            _ = opts.DB.Get(&allowed, `SELECT EXISTS (
              SELECT 1 FROM drivers d
              JOIN deliveries dv ON dv.driver_id=d.id AND dv.vehicle_id=$1 AND dv.status <> 'completed'
              WHERE d.user_id=$2
            )`, vid, uid)
        }
        if !allowed { return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false}) }
        lim, _ := strconv.Atoi(c.Query("limit", "100")); if lim <= 0 || lim > 1000 { lim = 100 }
        rows := []struct{ Lat float64 `db:"lat" json:"lat"`; Lng float64 `db:"lng" json:"lng"`; Ts time.Time `db:"ts" json:"ts"` }{}
        if err := opts.DB.Select(&rows, `SELECT lat, lng, ts FROM device_positions WHERE vehicle_id=$1 ORDER BY ts DESC LIMIT $2`, vid, lim); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": rows})
    })
    app.Get("/v1/assets/:id/positions", func(c *fiber.Ctx) error {
        aid := c.Params("id"); uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var allowed bool
        _ = opts.DB.Get(&allowed, `SELECT EXISTS (SELECT 1 FROM assets WHERE id=$1 AND owner_user_id=$2)`, aid, uid)
        if !allowed { return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false}) }
        lim, _ := strconv.Atoi(c.Query("limit", "100")); if lim <= 0 || lim > 1000 { lim = 100 }
        rows := []struct{ Lat float64 `db:"lat" json:"lat"`; Lng float64 `db:"lng" json:"lng"`; Ts time.Time `db:"ts" json:"ts"` }{}
        if err := opts.DB.Select(&rows, `SELECT lat, lng, ts FROM device_positions WHERE asset_id=$1 ORDER BY ts DESC LIMIT $2`, aid, lim); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": rows})
    })

    // Admin retention cleanup
    app.Post("/v1/admin/positions/cleanup", func(c *fiber.Ctx) error {
        uid := auth.UserID(c)
        var isAdmin bool
        _ = opts.DB.Get(&isAdmin, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role='admin')`, uid)
        if !isAdmin { return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false}) }
        days, _ := strconv.Atoi(c.Query("before_days", "90"))
        if days < 7 { days = 7 }
        _, err := opts.DB.Exec(`DELETE FROM device_positions WHERE ts < (now() - ($1 || ' days')::interval)`, days)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })
    app.Get("/v1/assets", func(c *fiber.Ctx) error {
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        rows := []struct { ID string `db:"id" json:"id"`; Kind string `db:"kind" json:"kind"`; Name *string `db:"name" json:"name"`; Identifier *string `db:"identifier" json:"identifier"`; Lat *float64 `db:"lat" json:"lat"`; Lng *float64 `db:"lng" json:"lng"` }{}
        if err := opts.DB.Select(&rows, `SELECT id, kind, name, identifier, lat, lng FROM assets WHERE owner_user_id=$1 ORDER BY kind, name NULLS LAST`, uid); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": rows})
    })
    app.Post("/v1/assets/:id/track", func(c *fiber.Ctx) error {
        aid := c.Params("id")
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var own bool
        if err := opts.DB.Get(&own, `SELECT EXISTS (SELECT 1 FROM assets WHERE id=$1 AND owner_user_id=$2)`, aid, uid); err != nil || !own {
            return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false})
        }
        var in struct { Lat float64 `json:"lat"`; Lng float64 `json:"lng"` }
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        _, err := opts.DB.Exec(`UPDATE assets SET lat=$1, lng=$2, last_seen=now(), updated_at=now() WHERE id=$3`, in.Lat, in.Lng, aid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        broadcastPosition("asset", aid, in.Lat, in.Lng, time.Now().UTC())
        return c.JSON(fiber.Map{"success": true})
    })
    app.Post("/v1/assets/:id/devices", func(c *fiber.Ctx) error {
        aid := c.Params("id")
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var own bool
        if err := opts.DB.Get(&own, `SELECT EXISTS (SELECT 1 FROM assets WHERE id=$1 AND owner_user_id=$2)`, aid, uid); err != nil || !own {
            return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false})
        }
        var in struct{ Name string `json:"name"`; IMEI *string `json:"imei"`; Serial *string `json:"serial"`; Kind *string `json:"kind"` }
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        buf := make([]byte, 32); _, _ = rand.Read(buf)
        token := hex.EncodeToString(buf)
        hash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
        var out struct{ ID string `db:"id" json:"id"`; Secret string `json:"secret"` }
        if err := opts.DB.Get(&out, `INSERT INTO devices (asset_id, name, imei, serial, kind, secret)
           VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, secret`, aid, in.Name, in.IMEI, in.Serial, in.Kind, string(hash)); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        out.Secret = token
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": out})
    })
    app.Post("/v1/assets/:id/assign", func(c *fiber.Ctx) error {
        aid := c.Params("id"); uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in struct{ VehicleID string `json:"vehicleId"` }
        if err := c.BodyParser(&in); err != nil || in.VehicleID == "" { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        // Ensure asset belongs to user and vehicle belongs to user
        var ok1, ok2 bool
        _ = opts.DB.Get(&ok1, `SELECT EXISTS (SELECT 1 FROM assets WHERE id=$1 AND owner_user_id=$2)`, aid, uid)
        _ = opts.DB.Get(&ok2, `SELECT EXISTS (SELECT 1 FROM vehicles WHERE id=$1 AND owner_user_id=$2)`, in.VehicleID, uid)
        if !ok1 || !ok2 { return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false}) }
        _, err := opts.DB.Exec(`INSERT INTO asset_assignments (asset_id, vehicle_id) VALUES ($1,$2)`, aid, in.VehicleID)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })
    app.Post("/v1/assets/:id/unassign", func(c *fiber.Ctx) error {
        aid := c.Params("id"); uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var ok bool
        _ = opts.DB.Get(&ok, `SELECT EXISTS (SELECT 1 FROM assets WHERE id=$1 AND owner_user_id=$2)`, aid, uid)
        if !ok { return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false}) }
        _, err := opts.DB.Exec(`UPDATE asset_assignments SET unassigned_at=now() WHERE asset_id=$1 AND unassigned_at IS NULL`, aid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })
    // Vehicles: register tracking device (owner only)
    app.Post("/v1/vehicles/:id/devices", func(c *fiber.Ctx) error {
        vid := c.Params("id")
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        // Verify ownership
        var isOwner bool
        if err := opts.DB.Get(&isOwner, `SELECT EXISTS (SELECT 1 FROM vehicles WHERE id=$1 AND owner_user_id=$2)`, vid, uid); err != nil || !isOwner {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        var in struct{ Name string `json:"name"`; IMEI *string `json:"imei"`; Serial *string `json:"serial"`; Kind *string `json:"kind"` }
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        // Generate device token
        buf := make([]byte, 32); _, _ = rand.Read(buf)
        token := hex.EncodeToString(buf)
        hash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
        var out struct{ ID string `db:"id" json:"id"`; Secret string `json:"secret"` }
        if err := opts.DB.Get(&out, `INSERT INTO devices (vehicle_id, name, imei, serial, kind, secret)
           VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, secret`, vid, in.Name, in.IMEI, in.Serial, in.Kind, string(hash)); err != nil {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
        }
        out.Secret = token
        return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": out})
    })

    // Vehicles: rotate device secret (owner only)
    app.Post("/v1/vehicles/:id/devices/:device/rotate", func(c *fiber.Ctx) error {
        vid := c.Params("id"); did := c.Params("device")
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var isOwner bool
        if err := opts.DB.Get(&isOwner, `SELECT EXISTS (SELECT 1 FROM vehicles WHERE id=$1 AND owner_user_id=$2)`, vid, uid); err != nil || !isOwner {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        buf := make([]byte, 32); _, _ = rand.Read(buf)
        token := hex.EncodeToString(buf)
        hash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
        _, err := opts.DB.Exec(`UPDATE devices SET secret=$1, updated_at=now() WHERE id=$2 AND vehicle_id=$3`, string(hash), did, vid)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true, "data": fiber.Map{"secret": token}})
    })

    // Device telemetry ingest (HTTP): authenticated by device credentials
    app.Post("/v1/ingest/telemetry", func(c *fiber.Ctx) error {
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        devID := c.Get("X-Device-Id")
        token := c.Get("X-Device-Token")
        if devID == "" || token == "" {
            return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
        }
        var devRow struct{ ID string `db:"id"`; VehicleID *string `db:"vehicle_id"`; AssetID *string `db:"asset_id"`; Secret string `db:"secret"` }
        if err := opts.DB.Get(&devRow, `SELECT id, vehicle_id, asset_id, secret FROM devices WHERE id=$1 AND status='active'`, devID); err != nil {
            return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
        }
        if err := bcrypt.CompareHashAndPassword([]byte(devRow.Secret), []byte(token)); err != nil {
            return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
        }
        // Support single fix or batch under { data: [...] }
        type fix struct {
            Lat float64 `json:"lat"`
            Lng float64 `json:"lng"`
            Speed *float64 `json:"speed"`
            Heading *float64 `json:"heading"`
            Altitude *float64 `json:"altitude"`
            HDOP *float64 `json:"hdop"`
            Sats *int `json:"sats"`
            Ts *time.Time `json:"ts"`
            Raw map[string]any `json:"raw"`
        }
        var envelope struct { Data []fix `json:"data"`; fix }
        if err := c.BodyParser(&envelope); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        fixes := envelope.Data
        if len(fixes) == 0 && envelope.fix.Lat != 0 && envelope.fix.Lng != 0 {
            fixes = []fix{ envelope.fix }
        }
        if len(fixes) == 0 { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "no positions"}) }
        for _, f := range fixes {
            ts := time.Now().UTC(); if f.Ts != nil { ts = (*f.Ts).UTC() }
            _, err := opts.DB.Exec(`INSERT INTO device_positions (device_id, vehicle_id, asset_id, lat, lng, speed, heading, altitude, hdop, sats, ts, raw)
              VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, devID, devRow.VehicleID, devRow.AssetID, f.Lat, f.Lng, f.Speed, f.Heading, f.Altitude, f.HDOP, f.Sats, ts, f.Raw)
            if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
            if devRow.VehicleID != nil {
                _, _ = opts.DB.Exec(`UPDATE vehicles SET lat=$1, lng=$2, last_seen=now(), updated_at=now() WHERE id=$3`, f.Lat, f.Lng, *devRow.VehicleID)
                broadcastPosition("vehicle", *devRow.VehicleID, f.Lat, f.Lng, ts)
            }
            if devRow.AssetID != nil {
                _, _ = opts.DB.Exec(`UPDATE assets SET lat=$1, lng=$2, last_seen=now(), updated_at=now() WHERE id=$3`, f.Lat, f.Lng, *devRow.AssetID)
                broadcastPosition("asset", *devRow.AssetID, f.Lat, f.Lng, ts)
            }
        }
        return c.JSON(fiber.Map{"success": true, "count": len(fixes)})
    })

    
    // Driver role flow
    app.Post("/v1/drivers/apply", func(c *fiber.Ctx) error {
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var in struct { FullName *string `json:"fullName"`; LicenseNo *string `json:"licenseNo"`; Phone *string `json:"phone"` }
        _ = c.BodyParser(&in)
        // Upsert driver application with pending status
        _, err := opts.DB.Exec(`INSERT INTO drivers (user_id, full_name, license_no, phone, status)
          VALUES ($1, COALESCE($2,''), $3, $4, 'pending')
          ON CONFLICT (id) DO NOTHING`, uid, in.FullName, in.LicenseNo, in.Phone)
        if err != nil {
            // If exists, update
            _, _ = opts.DB.Exec(`UPDATE drivers SET full_name=COALESCE($1, full_name), license_no=COALESCE($2, license_no), phone=COALESCE($3, phone), updated_at=now() WHERE user_id=$4`, in.FullName, in.LicenseNo, in.Phone, uid)
        }
        return c.JSON(fiber.Map{"success": true})
    })
    app.Get("/v1/drivers/me", func(c *fiber.Ctx) error {
        uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var d struct { ID string `db:"id" json:"id"`; Status string `db:"status" json:"status"`; FullName string `db:"full_name" json:"fullName"`; LicenseNo *string `db:"license_no" json:"licenseNo"`; Phone *string `db:"phone" json:"phone"`; Permissions map[string]any `db:"permissions" json:"permissions"` }
        if err := opts.DB.Get(&d, `SELECT id, status, full_name, license_no, phone, COALESCE(permissions,'{}'::jsonb) AS permissions FROM drivers WHERE user_id=$1`, uid); err != nil {
            return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
        }
        return c.JSON(fiber.Map{"success": true, "data": d})
    })
    // Admin-only approve/suspend and set driver permissions
    app.Post("/v1/drivers/:id/verify", func(c *fiber.Ctx) error {
        did := c.Params("id"); uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var isAdmin bool
        _ = opts.DB.Get(&isAdmin, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role='admin')`, uid)
        if !isAdmin { return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false}) }
        _, err := opts.DB.Exec(`UPDATE drivers SET status='verified', updated_at=now() WHERE id=$1`, did)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        // add global role 'driver'
        _, _ = opts.DB.Exec(`INSERT INTO user_roles (user_id, role)
          SELECT user_id, 'driver' FROM drivers WHERE id=$1
          ON CONFLICT (user_id, role) DO NOTHING`, did)
        return c.JSON(fiber.Map{"success": true})
    })
    app.Post("/v1/drivers/:id/permissions", func(c *fiber.Ctx) error {
        did := c.Params("id"); uid := auth.UserID(c)
        if opts.DB == nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        var isAdmin bool
        _ = opts.DB.Get(&isAdmin, `SELECT EXISTS (SELECT 1 FROM user_roles WHERE user_id=$1 AND role='admin')`, uid)
        if !isAdmin { return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"success": false}) }
        var in struct { Permissions map[string]any `json:"permissions"` }
        if err := c.BodyParser(&in); err != nil { return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false}) }
        _, err := opts.DB.Exec(`UPDATE drivers SET permissions=COALESCE($1,'{}'::jsonb), updated_at=now() WHERE id=$2`, in.Permissions, did)
        if err != nil { return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false}) }
        return c.JSON(fiber.Map{"success": true})
    })

    return app
}
