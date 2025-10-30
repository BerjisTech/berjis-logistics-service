package server

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/berjistech/berjis-ecosystem/logistics/service/internal/auth"
)

func registerFinanceRoutes(app *fiber.App, opts Options) {
	const (
		defaultInvoiceLimit = 50
		maxInvoiceLimit     = 200
		defaultLedgerLimit  = 50
		maxLedgerLimit      = 200
	)

	app.Get("/v1/finance/invoices", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		status := strings.TrimSpace(c.Query("status"))
		query := strings.TrimSpace(c.Query("q"))
		limit := clampInt(c.QueryInt("limit", defaultInvoiceLimit), 1, maxInvoiceLimit)
		offset := c.QueryInt("offset", 0)
		if offset < 0 {
			offset = 0
		}

		sqlBuilder := strings.Builder{}
		sqlBuilder.WriteString(`
			SELECT
				i.id,
				i.owner_user_id,
				i.customer_contact_id,
				i.reference,
				i.status,
				i.currency,
				i.subtotal,
				i.tax_amount,
				i.total_amount,
				i.amount_due,
				i.issued_at,
				i.due_at,
				i.paid_at,
				i.notes,
				i.created_at,
				i.updated_at,
				c.name AS customer_name,
				c.email AS customer_email,
				c.phone AS customer_phone,
				c.company AS customer_company,
				COALESCE(p.paid_total, 0) AS paid_total,
				CASE
					WHEN i.amount_due <= 0 THEN 'paid'
					WHEN i.due_at IS NOT NULL AND i.due_at < NOW() THEN 'overdue'
					ELSE 'open'
				END AS outstanding_state
			FROM invoices i
			LEFT JOIN contacts c ON c.id = i.customer_contact_id
			LEFT JOIN LATERAL (
				SELECT COALESCE(SUM(amount),0) AS paid_total
				FROM invoice_payments ip
				WHERE ip.invoice_id = i.id
				  AND ip.status IN ('succeeded','completed','paid')
			) p ON TRUE
			WHERE i.owner_user_id = $1`)

		args := []any{uid}
		argPos := 2
		if status != "" {
			sqlBuilder.WriteString(fmt.Sprintf(" AND i.status = $%d", argPos))
			args = append(args, status)
			argPos++
		}
		if query != "" {
			qLike := "%" + query + "%"
			sqlBuilder.WriteString(fmt.Sprintf(` AND (
				i.reference ILIKE $%d OR
				c.name ILIKE $%d OR
				c.company ILIKE $%d
			)`, argPos, argPos, argPos))
			args = append(args, qLike)
			argPos++
		}
		sqlBuilder.WriteString(fmt.Sprintf(" ORDER BY i.updated_at DESC, i.created_at DESC LIMIT $%d OFFSET $%d", argPos, argPos+1))
		args = append(args, limit, offset)

		rows := []invoiceRow{}
		if err := opts.DB.Select(&rows, sqlBuilder.String(), args...); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to list invoices"})
		}
		return c.JSON(fiber.Map{"success": true, "data": rows})
	})

	app.Post("/v1/finance/invoices", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		var in struct {
			CustomerContactID *string `json:"customerContactId"`
			Reference         *string `json:"reference"`
			Status            *string `json:"status"`
			Currency          *string `json:"currency"`
			IssuedAtRaw       *string `json:"issuedAt"`
			DueAtRaw          *string `json:"dueAt"`
			Notes             *string `json:"notes"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}

		if in.CustomerContactID != nil && !contactBelongsTo(opts, *in.CustomerContactID, uid) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid customer contact"})
		}
		status := "draft"
		if in.Status != nil && financeStatusValid(*in.Status) {
			status = *in.Status
		}
		currency := "USD"
		if in.Currency != nil && strings.TrimSpace(*in.Currency) != "" {
			currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
		}

		issuedAt, err := parseTimePtr(in.IssuedAtRaw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid issuedAt"})
		}
		dueAt, err := parseTimePtr(in.DueAtRaw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid dueAt"})
		}
		ref := normalizeStringPtr(in.Reference)
		notes := normalizeStringPtr(in.Notes)

		var row invoiceRow
		err = opts.DB.Get(&row, `
			INSERT INTO invoices (owner_user_id, customer_contact_id, reference, status, currency, issued_at, due_at, notes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, owner_user_id, customer_contact_id, reference, status, currency, subtotal, tax_amount, total_amount,
				amount_due, issued_at, due_at, paid_at, notes, created_at, updated_at,
				NULL::text AS customer_name, NULL::text AS customer_email, NULL::text AS customer_phone, NULL::text AS customer_company,
				0 AS paid_total, 'open' AS outstanding_state`,
			uid, in.CustomerContactID, ref, status, currency, issuedAt, dueAt, notes)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to create"})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": row})
	})

	app.Put("/v1/finance/invoices/:id", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		if id == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false})
		}

		var in struct {
			CustomerContactID *string `json:"customerContactId"`
			Reference         *string `json:"reference"`
			Status            *string `json:"status"`
			Currency          *string `json:"currency"`
			IssuedAtRaw       *string `json:"issuedAt"`
			DueAtRaw          *string `json:"dueAt"`
			Notes             *string `json:"notes"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}
		if !invoiceBelongsTo(opts, id, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		if in.CustomerContactID != nil && !contactBelongsTo(opts, *in.CustomerContactID, uid) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid customer contact"})
		}

		var status *string
		if in.Status != nil && financeStatusValid(*in.Status) {
			s := *in.Status
			status = &s
		}
		var currency *string
		if in.Currency != nil && strings.TrimSpace(*in.Currency) != "" {
			c := strings.ToUpper(strings.TrimSpace(*in.Currency))
			currency = &c
		}
		issuedAt, err := parseTimePtr(in.IssuedAtRaw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid issuedAt"})
		}
		dueAt, err := parseTimePtr(in.DueAtRaw)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid dueAt"})
		}
		ref := normalizeStringPtr(in.Reference)
		notes := normalizeStringPtr(in.Notes)

		var row invoiceRow
		err = opts.DB.Get(&row, `
			UPDATE invoices SET
				customer_contact_id = COALESCE($2, customer_contact_id),
				reference = COALESCE($3, reference),
				status = COALESCE($4, status),
				currency = COALESCE($5, currency),
				issued_at = COALESCE($6, issued_at),
				due_at = COALESCE($7, due_at),
				notes = COALESCE($8, notes),
				updated_at = NOW()
			WHERE id=$1 AND owner_user_id=$9
			RETURNING id, owner_user_id, customer_contact_id, reference, status, currency, subtotal, tax_amount, total_amount,
				amount_due, issued_at, due_at, paid_at, notes, created_at, updated_at,
				NULL::text AS customer_name, NULL::text AS customer_email, NULL::text AS customer_phone, NULL::text AS customer_company,
				0 AS paid_total, 'open' AS outstanding_state`,
			id, in.CustomerContactID, ref, status, currency, issuedAt, dueAt, notes, uid)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to update"})
		}
		_ = recalcInvoiceTotals(opts, id)
		return c.JSON(fiber.Map{"success": true, "data": row})
	})

	app.Get("/v1/finance/invoices/:id", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		id := c.Params("id")
		if id == "" || !invoiceBelongsTo(opts, id, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var inv invoiceRow
		err := opts.DB.Get(&inv, `
			SELECT
				i.id,
				i.owner_user_id,
				i.customer_contact_id,
				i.reference,
				i.status,
				i.currency,
				i.subtotal,
				i.tax_amount,
				i.total_amount,
				i.amount_due,
				i.issued_at,
				i.due_at,
				i.paid_at,
				i.notes,
				i.created_at,
				i.updated_at,
				c.name AS customer_name,
				c.email AS customer_email,
				c.phone AS customer_phone,
				c.company AS customer_company,
				COALESCE(p.paid_total, 0) AS paid_total,
				CASE
					WHEN i.amount_due <= 0 THEN 'paid'
					WHEN i.due_at IS NOT NULL AND i.due_at < NOW() THEN 'overdue'
					ELSE 'open'
				END AS outstanding_state
			FROM invoices i
			LEFT JOIN contacts c ON c.id = i.customer_contact_id
			LEFT JOIN LATERAL (
				SELECT COALESCE(SUM(amount),0) AS paid_total
				FROM invoice_payments ip
				WHERE ip.invoice_id = i.id
				  AND ip.status IN ('succeeded','completed','paid')
			) p ON TRUE
			WHERE i.id=$1 AND i.owner_user_id=$2`, id, uid)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		lines := []invoiceLineRow{}
		if err := opts.DB.Select(&lines, `SELECT id, invoice_id, description, quantity, unit_price, tax_rate, created_at FROM invoice_lines WHERE invoice_id=$1 ORDER BY created_at ASC`, id); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		payments := []invoicePaymentRow{}
		if err := opts.DB.Select(&payments, `SELECT id, invoice_id, owner_user_id, method, amount, currency, status, reference, processed_at, created_at FROM invoice_payments WHERE invoice_id=$1 ORDER BY created_at DESC`, id); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data": invoiceDetailRow{
				Invoice:  inv,
				Lines:    lines,
				Payments: payments,
			},
		})
	})

	app.Post("/v1/finance/invoices/:id/lines", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		invoiceID := c.Params("id")
		if invoiceID == "" || !invoiceBelongsTo(opts, invoiceID, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var in struct {
			Description string   `json:"description"`
			Quantity    *float64 `json:"quantity"`
			UnitPrice   *float64 `json:"unitPrice"`
			TaxRate     *float64 `json:"taxRate"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Description) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "description required"})
		}
		qty := 1.0
		if in.Quantity != nil && !math.IsNaN(*in.Quantity) {
			qty = *in.Quantity
		}
		price := 0.0
		if in.UnitPrice != nil && !math.IsNaN(*in.UnitPrice) {
			price = *in.UnitPrice
		}
		tax := 0.0
		if in.TaxRate != nil && !math.IsNaN(*in.TaxRate) {
			tax = *in.TaxRate
		}
		var line invoiceLineRow
		err := opts.DB.Get(&line, `
			INSERT INTO invoice_lines (invoice_id, description, quantity, unit_price, tax_rate)
			VALUES ($1,$2,$3,$4,$5)
			RETURNING id, invoice_id, description, quantity, unit_price, tax_rate, created_at`,
			invoiceID, strings.TrimSpace(in.Description), qty, price, tax)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to add line"})
		}
		_ = recalcInvoiceTotals(opts, invoiceID)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": line})
	})

	app.Delete("/v1/finance/invoices/:id/lines/:line", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		invoiceID := c.Params("id")
		lineID := c.Params("line")
		if invoiceID == "" || lineID == "" || !invoiceBelongsTo(opts, invoiceID, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		res, err := opts.DB.Exec(`DELETE FROM invoice_lines WHERE id=$1 AND invoice_id=$2`, lineID, invoiceID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		_ = recalcInvoiceTotals(opts, invoiceID)
		return c.JSON(fiber.Map{"success": true})
	})

	app.Post("/v1/finance/invoices/:id/payments", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		invoiceID := c.Params("id")
		if invoiceID == "" || !invoiceBelongsTo(opts, invoiceID, uid) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"success": false})
		}
		var in struct {
			Amount      *float64 `json:"amount"`
			Method      *string  `json:"method"`
			Currency    *string  `json:"currency"`
			Status      *string  `json:"status"`
			Reference   *string  `json:"reference"`
			ProcessedAt *string  `json:"processedAt"`
		}
		if err := c.BodyParser(&in); err != nil || in.Amount == nil || *in.Amount <= 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "amount required"})
		}
		method := normalizeStringPtr(in.Method)
		ref := normalizeStringPtr(in.Reference)
		status := "pending"
		if in.Status != nil && strings.TrimSpace(*in.Status) != "" {
			status = strings.ToLower(strings.TrimSpace(*in.Status))
		}
		currency := normalizeCurrencyPtr(in.Currency)
		processedAt, err := parseTimePtr(in.ProcessedAt)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid processedAt"})
		}
		var payment invoicePaymentRow
		err = opts.DB.Get(&payment, `
			INSERT INTO invoice_payments (invoice_id, owner_user_id, method, amount, currency, status, reference, processed_at)
			VALUES ($1,$2,$3,$4,COALESCE($5, 'USD'),$6,$7,$8)
			RETURNING id, invoice_id, owner_user_id, method, amount, currency, status, reference, processed_at, created_at`,
			invoiceID, uid, method, *in.Amount, currency, status, ref, processedAt)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to record payment"})
		}
		_ = recalcInvoiceTotals(opts, invoiceID)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": payment})
	})

	app.Get("/v1/finance/payments", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		status := strings.TrimSpace(c.Query("status"))
		invoiceID := strings.TrimSpace(c.Query("invoiceId"))
		limit := clampInt(c.QueryInt("limit", defaultInvoiceLimit), 1, maxInvoiceLimit)
		offset := c.QueryInt("offset", 0)
		if offset < 0 {
			offset = 0
		}
		sqlBuilder := strings.Builder{}
		sqlBuilder.WriteString(`
			SELECT ip.id, ip.invoice_id, ip.owner_user_id, ip.method, ip.amount, ip.currency, ip.status, ip.reference, ip.processed_at, ip.created_at
			FROM invoice_payments ip
			JOIN invoices i ON i.id = ip.invoice_id
			WHERE i.owner_user_id = $1`)
		args := []any{uid}
		argPos := 2
		if status != "" {
			sqlBuilder.WriteString(fmt.Sprintf(" AND ip.status = $%d", argPos))
			args = append(args, status)
			argPos++
		}
		if invoiceID != "" {
			sqlBuilder.WriteString(fmt.Sprintf(" AND ip.invoice_id = $%d", argPos))
			args = append(args, invoiceID)
			argPos++
		}
		sqlBuilder.WriteString(fmt.Sprintf(" ORDER BY ip.created_at DESC LIMIT $%d OFFSET $%d", argPos, argPos+1))
		args = append(args, limit, offset)

		payments := []invoicePaymentRow{}
		if err := opts.DB.Select(&payments, sqlBuilder.String(), args...); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": payments})
	})

	app.Get("/v1/finance/ledger", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		kind := strings.TrimSpace(c.Query("kind"))
		fromStr := strings.TrimSpace(c.Query("from"))
		toStr := strings.TrimSpace(c.Query("to"))
		fromTime, err := parseTimePtr(optStringPtr(fromStr))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid from"})
		}
		toTime, err := parseTimePtr(optStringPtr(toStr))
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid to"})
		}
		limit := clampInt(c.QueryInt("limit", defaultLedgerLimit), 1, maxLedgerLimit)
		offset := c.QueryInt("offset", 0)
		if offset < 0 {
			offset = 0
		}

		sqlBuilder := strings.Builder{}
		sqlBuilder.WriteString(`
			SELECT id, owner_user_id, kind, reference, amount, currency, occurred_at, metadata, created_at
			FROM finance_ledger
			WHERE owner_user_id = $1`)
		args := []any{uid}
		argPos := 2
		if kind != "" {
			sqlBuilder.WriteString(fmt.Sprintf(" AND kind = $%d", argPos))
			args = append(args, kind)
			argPos++
		}
		if fromTime != nil {
			sqlBuilder.WriteString(fmt.Sprintf(" AND occurred_at >= $%d", argPos))
			args = append(args, *fromTime)
			argPos++
		}
		if toTime != nil {
			sqlBuilder.WriteString(fmt.Sprintf(" AND occurred_at <= $%d", argPos))
			args = append(args, *toTime)
			argPos++
		}
		sqlBuilder.WriteString(fmt.Sprintf(" ORDER BY occurred_at DESC LIMIT $%d OFFSET $%d", argPos, argPos+1))
		args = append(args, limit, offset)

		rows := []ledgerEntryRow{}
		if err := opts.DB.Select(&rows, sqlBuilder.String(), args...); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		return c.JSON(fiber.Map{"success": true, "data": rows})
	})

	app.Post("/v1/finance/ledger", func(c *fiber.Ctx) error {
		if opts.DB == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false})
		}
		uid := auth.UserID(c)
		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false})
		}
		var in struct {
			Kind      string         `json:"kind"`
			Reference *string        `json:"reference"`
			Amount    float64        `json:"amount"`
			Currency  *string        `json:"currency"`
			Occurred  *string        `json:"occurredAt"`
			Metadata  map[string]any `json:"metadata"`
		}
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.Kind) == "" || in.Amount == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid body"})
		}
		kind := strings.ToLower(strings.TrimSpace(in.Kind))
		if !ledgerKindValid(kind) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid kind"})
		}
		currency := "USD"
		if in.Currency != nil && strings.TrimSpace(*in.Currency) != "" {
			currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
		}
		occurredAt, err := parseTimePtr(in.Occurred)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid occurredAt"})
		}
		ref := normalizeStringPtr(in.Reference)
		metaBytes, err := json.Marshal(in.Metadata)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "message": "invalid metadata"})
		}

		var row ledgerEntryRow
		err = opts.DB.Get(&row, `
			INSERT INTO finance_ledger (owner_user_id, kind, reference, amount, currency, occurred_at, metadata)
			VALUES ($1,$2,$3,$4,$5,COALESCE($6, NOW()), COALESCE($7::jsonb, '{}'::jsonb))
			RETURNING id, owner_user_id, kind, reference, amount, currency, occurred_at, metadata, created_at`,
			uid, kind, ref, in.Amount, currency, occurredAt, string(metaBytes))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"success": false, "message": "failed to create ledger entry"})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": row})
	})
}

type invoiceRow struct {
	ID                string     `db:"id" json:"id"`
	OwnerUserID       string     `db:"owner_user_id" json:"ownerUserId"`
	CustomerContactID *string    `db:"customer_contact_id" json:"customerContactId,omitempty"`
	Reference         *string    `db:"reference" json:"reference,omitempty"`
	Status            string     `db:"status" json:"status"`
	Currency          string     `db:"currency" json:"currency"`
	Subtotal          float64    `db:"subtotal" json:"subtotal"`
	TaxAmount         float64    `db:"tax_amount" json:"taxAmount"`
	TotalAmount       float64    `db:"total_amount" json:"totalAmount"`
	AmountDue         float64    `db:"amount_due" json:"amountDue"`
	IssuedAt          *time.Time `db:"issued_at" json:"issuedAt,omitempty"`
	DueAt             *time.Time `db:"due_at" json:"dueAt,omitempty"`
	PaidAt            *time.Time `db:"paid_at" json:"paidAt,omitempty"`
	Notes             *string    `db:"notes" json:"notes,omitempty"`
	CreatedAt         time.Time  `db:"created_at" json:"createdAt"`
	UpdatedAt         time.Time  `db:"updated_at" json:"updatedAt"`
	CustomerName      *string    `db:"customer_name" json:"customerName,omitempty"`
	CustomerEmail     *string    `db:"customer_email" json:"customerEmail,omitempty"`
	CustomerPhone     *string    `db:"customer_phone" json:"customerPhone,omitempty"`
	CustomerCompany   *string    `db:"customer_company" json:"customerCompany,omitempty"`
	PaidTotal         float64    `db:"paid_total" json:"paidTotal"`
	OutstandingState  string     `db:"outstanding_state" json:"outstandingState"`
}

type invoiceLineRow struct {
	ID          string    `db:"id" json:"id"`
	InvoiceID   string    `db:"invoice_id" json:"invoiceId"`
	Description string    `db:"description" json:"description"`
	Quantity    float64   `db:"quantity" json:"quantity"`
	UnitPrice   float64   `db:"unit_price" json:"unitPrice"`
	TaxRate     float64   `db:"tax_rate" json:"taxRate"`
	CreatedAt   time.Time `db:"created_at" json:"createdAt"`
}

type invoicePaymentRow struct {
	ID          string     `db:"id" json:"id"`
	InvoiceID   string     `db:"invoice_id" json:"invoiceId"`
	OwnerUserID string     `db:"owner_user_id" json:"ownerUserId"`
	Method      *string    `db:"method" json:"method,omitempty"`
	Amount      float64    `db:"amount" json:"amount"`
	Currency    string     `db:"currency" json:"currency"`
	Status      string     `db:"status" json:"status"`
	Reference   *string    `db:"reference" json:"reference,omitempty"`
	ProcessedAt *time.Time `db:"processed_at" json:"processedAt,omitempty"`
	CreatedAt   time.Time  `db:"created_at" json:"createdAt"`
}

type invoiceDetailRow struct {
	Invoice  invoiceRow          `json:"invoice"`
	Lines    []invoiceLineRow    `json:"lines"`
	Payments []invoicePaymentRow `json:"payments"`
}

type ledgerEntryRow struct {
	ID          string          `db:"id" json:"id"`
	OwnerUserID string          `db:"owner_user_id" json:"ownerUserId"`
	Kind        string          `db:"kind" json:"kind"`
	Reference   *string         `db:"reference" json:"reference,omitempty"`
	Amount      float64         `db:"amount" json:"amount"`
	Currency    string          `db:"currency" json:"currency"`
	OccurredAt  time.Time       `db:"occurred_at" json:"occurredAt"`
	Metadata    json.RawMessage `db:"metadata" json:"metadata"`
	CreatedAt   time.Time       `db:"created_at" json:"createdAt"`
}

func contactBelongsTo(opts Options, contactID, ownerID string) bool {
	if opts.DB == nil {
		return false
	}
	var exists bool
	_ = opts.DB.Get(&exists, `SELECT EXISTS(SELECT 1 FROM contacts WHERE id=$1 AND owner_user_id=$2)`, contactID, ownerID)
	return exists
}

func invoiceBelongsTo(opts Options, invoiceID, ownerID string) bool {
	if opts.DB == nil {
		return false
	}
	var exists bool
	_ = opts.DB.Get(&exists, `SELECT EXISTS(SELECT 1 FROM invoices WHERE id=$1 AND owner_user_id=$2)`, invoiceID, ownerID)
	return exists
}

func financeStatusValid(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "draft", "issued", "overdue", "paid", "void":
		return true
	default:
		return false
	}
}

func ledgerKindValid(kind string) bool {
	switch kind {
	case "revenue", "expense", "adjustment":
		return true
	default:
		return false
	}
}

func recalcInvoiceTotals(opts Options, invoiceID string) error {
	if opts.DB == nil {
		return nil
	}
	var sums struct {
		Subtotal float64 `db:"subtotal"`
		Tax      float64 `db:"tax"`
	}
	if err := opts.DB.Get(&sums, `
		SELECT
			COALESCE(SUM(quantity * unit_price), 0) AS subtotal,
			COALESCE(SUM(quantity * unit_price * (tax_rate/100.0)), 0) AS tax
		FROM invoice_lines
		WHERE invoice_id=$1`, invoiceID); err != nil {
		return err
	}
	var paid struct {
		Amount float64 `db:"amount"`
	}
	if err := opts.DB.Get(&paid, `
		SELECT COALESCE(SUM(amount), 0) AS amount
		FROM invoice_payments
		WHERE invoice_id=$1
		  AND status IN ('succeeded','completed','paid')`, invoiceID); err != nil {
		return err
	}
	total := sums.Subtotal + sums.Tax
	amountDue := total - paid.Amount
	if amountDue < 0 {
		amountDue = 0
	}
	_, err := opts.DB.Exec(`
		UPDATE invoices
		SET subtotal=$1,
			tax_amount=$2,
			total_amount=$3,
			amount_due=$4,
			paid_at = CASE WHEN $4 <= 0 THEN COALESCE(paid_at, NOW()) ELSE NULL END,
			updated_at=NOW()
		WHERE id=$5`, sums.Subtotal, sums.Tax, total, amountDue, invoiceID)
	return err
}
