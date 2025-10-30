package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/berjistech/berjis-ecosystem/logistics/service/internal/auth"
)

func registerMarketingRoutes(app *fiber.App, opts Options) {
	const (
		defaultCampaignLimit = 50
		maxCampaignLimit     = 200
		defaultEventLimit    = 100
	)

	app.Get("/v1/marketing/campaigns", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		status := strings.TrimSpace(c.Query("status"))
		query := strings.TrimSpace(c.Query("q"))
		limit := clampInt(c.QueryInt("limit", defaultCampaignLimit), 1, maxCampaignLimit)
		offset := c.QueryInt("offset", 0)
		if offset < 0 {
			offset = 0
		}

		sqlBuilder := strings.Builder{}
		sqlBuilder.WriteString(`
			SELECT
				c.id,
				c.owner_user_id,
				c.name,
				c.objective,
				c.channel,
				c.status,
				c.budget,
				c.spend,
				c.currency,
				c.starts_at,
				c.ends_at,
				c.created_at,
				c.updated_at,
				COALESCE(SUM(CASE WHEN e.event_type='impression' THEN 1 ELSE 0 END),0) AS impressions,
				COALESCE(SUM(CASE WHEN e.event_type='click' THEN 1 ELSE 0 END),0) AS clicks,
				COALESCE(SUM(CASE WHEN e.event_type='lead' THEN 1 ELSE 0 END),0) AS leads,
				COALESCE(SUM(CASE WHEN e.event_type='conversion' THEN 1 ELSE 0 END),0) AS conversions
			FROM marketing_campaigns c
			LEFT JOIN marketing_events e ON e.campaign_id = c.id
			WHERE c.owner_user_id = $1`)
		args := []any{uid}
		argPos := 2
		if status != "" {
			sqlBuilder.WriteString(fmt.Sprintf(" AND c.status = $%d", argPos))
			args = append(args, status)
			argPos++
		}
		if query != "" {
			qLike := "%" + query + "%"
			sqlBuilder.WriteString(fmt.Sprintf(` AND (
				c.name ILIKE $%d OR
				c.objective ILIKE $%d OR
				c.channel ILIKE $%d
			)`, argPos, argPos, argPos))
			args = append(args, qLike)
			argPos++
		}
		sqlBuilder.WriteString(` GROUP BY c.id`)
		sqlBuilder.WriteString(fmt.Sprintf(" ORDER BY c.updated_at DESC, c.created_at DESC LIMIT $%d OFFSET $%d", argPos, argPos+1))
		args = append(args, limit, offset)

		rows := []campaignRow{}
		if err := opts.DB.Select(&rows, sqlBuilder.String(), args...); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to list campaigns"})
		}
		return c.JSON(fiber.Map{"success": true, "data": rows})
	})

	app.Post("/v1/marketing/campaigns", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		var in struct {
			Name      string   `json:"name"`
			Objective *string  `json:"objective"`
			Channel   *string  `json:"channel"`
			Status    *string  `json:"status"`
			Budget    *float64 `json:"budget"`
			Currency  *string  `json:"currency"`
			StartsRaw *string  `json:"startsAt"`
			EndsRaw   *string  `json:"endsAt"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Name) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "name required"})
		}
		status := "draft"
		if in.Status != nil && campaignStatusValid(*in.Status) {
			status = strings.ToLower(strings.TrimSpace(*in.Status))
		}
		currency := "USD"
		if in.Currency != nil && strings.TrimSpace(*in.Currency) != "" {
			currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
		}
		startsAt, err := parseTimePtr(in.StartsRaw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid startsAt"})
		}
		endsAt, err := parseTimePtr(in.EndsRaw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid endsAt"})
		}
		objective := normalizeStringPtr(in.Objective)
		channel := normalizeStringPtr(in.Channel)

		var row campaignRow
		err = opts.DB.Get(&row, `
            INSERT INTO marketing_campaigns (owner_user_id, name, objective, channel, status, budget, spend, currency, starts_at, ends_at)
            VALUES ($1,$2,$3,$4,$5,$6,0,$7,$8,$9)
            RETURNING id, owner_user_id, name, objective, channel, status, budget, spend, currency, starts_at, ends_at, created_at, updated_at,
                0 AS impressions, 0 AS clicks, 0 AS leads, 0 AS conversions`,
			uid, strings.TrimSpace(in.Name), objective, channel, status, in.Budget, currency, startsAt, endsAt)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to create campaign"})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": row})
	})

	app.Put("/v1/marketing/campaigns/:id", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		if id == "" || !campaignBelongsTo(opts, id, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var in struct {
			Name      *string  `json:"name"`
			Objective *string  `json:"objective"`
			Channel   *string  `json:"channel"`
			Status    *string  `json:"status"`
			Budget    *float64 `json:"budget"`
			Currency  *string  `json:"currency"`
			StartsRaw *string  `json:"startsAt"`
			EndsRaw   *string  `json:"endsAt"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}
		var status *string
		if in.Status != nil && campaignStatusValid(*in.Status) {
			s := strings.ToLower(strings.TrimSpace(*in.Status))
			status = &s
		}
		var currency *string
		if in.Currency != nil && strings.TrimSpace(*in.Currency) != "" {
			c := strings.ToUpper(strings.TrimSpace(*in.Currency))
			currency = &c
		}
		startsAt, err := parseTimePtr(in.StartsRaw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid startsAt"})
		}
		endsAt, err := parseTimePtr(in.EndsRaw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid endsAt"})
		}
		name := normalizeStringPtr(in.Name)
		Objective := normalizeStringPtr(in.Objective)
		channel := normalizeStringPtr(in.Channel)

		var row campaignRow
		err = opts.DB.Get(&row, `
			UPDATE marketing_campaigns SET
				name = COALESCE($2, name),
				objective = COALESCE($3, objective),
				channel = COALESCE($4, channel),
				status = COALESCE($5, status),
				budget = COALESCE($6, budget),
				currency = COALESCE($7, currency),
				starts_at = COALESCE($8, starts_at),
				ends_at = COALESCE($9, ends_at),
				updated_at = NOW()
			WHERE id=$1 AND owner_user_id=$10
			RETURNING id, owner_user_id, name, objective, channel, status, budget, spend, currency, starts_at, ends_at, created_at, updated_at,
				0 AS impressions, 0 AS clicks, 0 AS leads, 0 AS conversions`,
			id, name, Objective, channel, status, in.Budget, currency, startsAt, endsAt, uid)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to update campaign"})
		}
		return c.JSON(fiber.Map{"success": true, "data": row})
	})

	app.Get("/v1/marketing/campaigns/:id", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		if id == "" || !campaignBelongsTo(opts, id, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var campaign campaignRow
		err := opts.DB.Get(&campaign, `
			SELECT
				c.id,
				c.owner_user_id,
				c.name,
				c.objective,
				c.channel,
				c.status,
				c.budget,
				c.spend,
				c.currency,
				c.starts_at,
				c.ends_at,
				c.created_at,
				c.updated_at,
				COALESCE(SUM(CASE WHEN e.event_type='impression' THEN 1 ELSE 0 END),0) AS impressions,
				COALESCE(SUM(CASE WHEN e.event_type='click' THEN 1 ELSE 0 END),0) AS clicks,
				COALESCE(SUM(CASE WHEN e.event_type='lead' THEN 1 ELSE 0 END),0) AS leads,
				COALESCE(SUM(CASE WHEN e.event_type='conversion' THEN 1 ELSE 0 END),0) AS conversions
			FROM marketing_campaigns c
			LEFT JOIN marketing_events e ON e.campaign_id = c.id
			WHERE c.id=$1 AND c.owner_user_id=$2
			GROUP BY c.id`, id, uid)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		targets := []campaignTargetRow{}
		if err := opts.DB.Select(&targets, `SELECT id, campaign_id, contact_id, segment, created_at FROM marketing_campaign_targets WHERE campaign_id=$1 ORDER BY created_at DESC`, id); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		events := []campaignEventRow{}
		if err := opts.DB.Select(&events, `SELECT id, campaign_id, event_type, value, occurred_at, metadata FROM marketing_events WHERE campaign_id=$1 ORDER BY occurred_at DESC LIMIT $2`, id, defaultEventLimit); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data": campaignDetailRow{
				Campaign: campaign,
				Targets:  targets,
				Events:   events,
			},
		})
	})

	app.Post("/v1/marketing/campaigns/:id/targets", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		if id == "" || !campaignBelongsTo(opts, id, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var in struct {
			ContactID *string `json:"contactId"`
			Segment   *string `json:"segment"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}
		if in.ContactID != nil && !contactBelongsTo(opts, *in.ContactID, uid) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid contact"})
		}
		segment := normalizeStringPtr(in.Segment)
		var target campaignTargetRow
		err := opts.DB.Get(&target, `
			INSERT INTO marketing_campaign_targets (campaign_id, contact_id, segment)
			VALUES ($1,$2,$3)
			RETURNING id, campaign_id, contact_id, segment, created_at`,
			id, in.ContactID, segment)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to add target"})
		}
		_, _ = opts.DB.Exec(`UPDATE marketing_campaigns SET updated_at=NOW() WHERE id=$1`, id)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": target})
	})

	app.Post("/v1/marketing/campaigns/:id/events", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		if id == "" || !campaignBelongsTo(opts, id, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var in struct {
			EventType string         `json:"eventType"`
			Value     *float64       `json:"value"`
			Occurred  *string        `json:"occurredAt"`
			Metadata  map[string]any `json:"metadata"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.EventType) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "eventType required"})
		}
		eventType := strings.ToLower(strings.TrimSpace(in.EventType))
		if !campaignEventTypeValid(eventType) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid eventType"})
		}
		occurredAt, err := parseTimePtr(in.Occurred)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid occurredAt"})
		}
		metaBytes, err := json.Marshal(in.Metadata)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid metadata"})
		}
		val := 0.0
		if in.Value != nil {
			val = *in.Value
		}

		var event campaignEventRow
		err = opts.DB.Get(&event, `
			INSERT INTO marketing_events (campaign_id, event_type, value, occurred_at, metadata)
			VALUES ($1,$2,$3,COALESCE($4, NOW()), COALESCE($5::jsonb, '{}'::jsonb))
			RETURNING id, campaign_id, event_type, value, occurred_at, metadata`,
			id, eventType, val, occurredAt, string(metaBytes))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to record event"})
		}
		if eventType == "spend" {
			_, _ = opts.DB.Exec(`UPDATE marketing_campaigns SET spend = COALESCE(spend,0) + $1, updated_at=NOW() WHERE id=$2`, val, id)
		} else {
			_, _ = opts.DB.Exec(`UPDATE marketing_campaigns SET updated_at=NOW() WHERE id=$1`, id)
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": event})
	})
}

type campaignRow struct {
	ID          string     `db:"id" json:"id"`
	OwnerUserID string     `db:"owner_user_id" json:"ownerUserId"`
	Name        string     `db:"name" json:"name"`
	Objective   *string    `db:"objective" json:"objective,omitempty"`
	Channel     *string    `db:"channel" json:"channel,omitempty"`
	Status      string     `db:"status" json:"status"`
	Budget      *float64   `db:"budget" json:"budget,omitempty"`
	Spend       float64    `db:"spend" json:"spend"`
	Currency    string     `db:"currency" json:"currency"`
	StartsAt    *time.Time `db:"starts_at" json:"startsAt,omitempty"`
	EndsAt      *time.Time `db:"ends_at" json:"endsAt,omitempty"`
	CreatedAt   time.Time  `db:"created_at" json:"createdAt"`
	UpdatedAt   time.Time  `db:"updated_at" json:"updatedAt"`
	Impressions int        `db:"impressions" json:"impressions"`
	Clicks      int        `db:"clicks" json:"clicks"`
	Leads       int        `db:"leads" json:"leads"`
	Conversions int        `db:"conversions" json:"conversions"`
}

type campaignTargetRow struct {
	ID         string    `db:"id" json:"id"`
	CampaignID string    `db:"campaign_id" json:"campaignId"`
	ContactID  *string   `db:"contact_id" json:"contactId,omitempty"`
	Segment    *string   `db:"segment" json:"segment,omitempty"`
	CreatedAt  time.Time `db:"created_at" json:"createdAt"`
}

type campaignEventRow struct {
	ID         string          `db:"id" json:"id"`
	CampaignID string          `db:"campaign_id" json:"campaignId"`
	EventType  string          `db:"event_type" json:"eventType"`
	Value      *float64        `db:"value" json:"value,omitempty"`
	OccurredAt time.Time       `db:"occurred_at" json:"occurredAt"`
	Metadata   json.RawMessage `db:"metadata" json:"metadata"`
}

type campaignDetailRow struct {
	Campaign campaignRow         `json:"campaign"`
	Targets  []campaignTargetRow `json:"targets"`
	Events   []campaignEventRow  `json:"events"`
}

func campaignBelongsTo(opts Options, campaignID, ownerID string) bool {
	if opts.DB == nil {
		return false
	}
	var exists bool
	_ = opts.DB.Get(&exists, `SELECT EXISTS(SELECT 1 FROM marketing_campaigns WHERE id=$1 AND owner_user_id=$2)`, campaignID, ownerID)
	return exists
}

func campaignStatusValid(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "draft", "scheduled", "running", "paused", "completed":
		return true
	default:
		return false
	}
}

func campaignEventTypeValid(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "impression", "click", "lead", "conversion", "spend":
		return true
	default:
		return false
	}
}
