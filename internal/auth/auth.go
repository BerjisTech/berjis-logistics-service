package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	coreauth "github.com/berjistech/berjis-ecosystem/shared/coreauth"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Options struct {
	HS256Secret string
	Env         string
	CoreAPIBase string
	HTTPClient  *http.Client
	Verifier    *coreauth.Verifier
}

type User struct {
	ID    string
	Email string
}

const userKey = "userID"

func Middleware(opts Options) fiber.Handler {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return func(c *fiber.Ctx) error {
		var uid string

		authz := strings.TrimSpace(c.Get("Authorization"))
		tokenStr := ""
		if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			tokenStr = strings.TrimSpace(authz[7:])
		}
		if tokenStr == "" {
			tokenStr = strings.TrimSpace(c.Cookies("access", ""))
		}

		// If we have a local secret configured, validate the bearer token locally first.
		if tokenPart := strings.TrimSpace(opts.HS256Secret); tokenPart != "" && strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			raw := strings.TrimSpace(authz[7:])
			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return []byte(opts.HS256Secret), nil
			})
			if err == nil && token.Valid {
				if sub, ok := claims["sub"].(string); ok {
					sub = strings.TrimSpace(sub)
					if _, err := uuid.Parse(sub); err == nil {
						uid = sub
					}
				}
			}
		}

		// Verify against Core JWTs via shared JWKS when available.
		if uid == "" && tokenStr != "" && opts.Verifier != nil {
			if claims, err := opts.Verifier.Verify(tokenStr); err == nil {
				uid = claims.UUID
			} else if errors.Is(err, coreauth.ErrTokenInvalid) || errors.Is(err, coreauth.ErrTokenExpired) || errors.Is(err, coreauth.ErrTokenMissing) {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false, "message": "login required"})
			}
		}

		// If still no UID, perform remote verify against Core API using cookies/authorization.
		if uid == "" && strings.TrimSpace(opts.CoreAPIBase) != "" {
			apiURL := strings.TrimRight(opts.CoreAPIBase, "/") + "/v1/auth/verify"
			req, _ := http.NewRequest(http.MethodPost, apiURL, nil)
			// Prefer Authorization if provided so service calls can authenticate via access tokens.
			if v := c.Get("Authorization"); v != "" {
				req.Header.Set("Authorization", v)
			}
			// Also forward cookies if the client happens to include Core cookies (e.g., same-site setups).
			if v := c.Get("Cookie"); v != "" {
				req.Header.Set("Cookie", v)
			}
			if v := c.Get("Origin"); v != "" {
				req.Header.Set("Origin", v)
			}
			req.Header.Set("Accept", "application/json")
			if resp, err := client.Do(req); err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var body map[string]any
					if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
						if data, ok := body["data"].(map[string]any); ok {
							if valid, vok := data["valid"].(bool); vok && valid {
								if uuidStr, ok := data["uuid"].(string); ok && uuidStr != "" {
									uid = uuidStr
								}
							}
						}
					}
				} else {
					// Drain body to reuse connections
					_, _ = io.Copy(io.Discard, resp.Body)
				}
			}
		}

		// Dev fallback: X-User-UUID header
		env := strings.ToLower(opts.Env)
		if uid == "" {
			candidate := strings.TrimSpace(c.Get("X-User-UUID"))
			if candidate != "" {
				if _, err := uuid.Parse(candidate); err == nil {
					uid = candidate
				} else if env == "development" {
					uid = candidate
				}
			}
		}

		if uid == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false, "message": "login required"})
		}
		if _, err := uuid.Parse(uid); err != nil && env != "development" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false, "message": "login required"})
		}
		c.Locals(userKey, uid)
		return c.Next()
	}
}

func UserID(c *fiber.Ctx) string {
	if v := c.Locals(userKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
