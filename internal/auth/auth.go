package auth

import (
    "errors"
    "strings"

    "github.com/gofiber/fiber/v2"
    "github.com/golang-jwt/jwt/v5"
)

type Options struct {
    HS256Secret string
    Env         string
}

type User struct {
    ID    string
    Email string
}

const userKey = "userID"

func Middleware(opts Options) fiber.Handler {
    return func(c *fiber.Ctx) error {
        // Prefer Authorization: Bearer <jwt>
        authz := c.Get("Authorization")
        var uid string
        if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
            tokenStr := strings.TrimSpace(authz[7:])
            if opts.HS256Secret == "" {
                return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false, "message": "auth misconfigured"})
            }
            claims := jwt.MapClaims{}
            token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
                if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
                    return nil, errors.New("unexpected signing method")
                }
                return []byte(opts.HS256Secret), nil
            })
            if err != nil || !token.Valid {
                return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false, "message": "invalid token"})
            }
            if sub, ok := claims["sub"].(string); ok && sub != "" {
                uid = sub
            }
        }
        // Dev fallback via X-User-ID
        if uid == "" && strings.ToLower(opts.Env) == "development" {
            uid = c.Get("X-User-ID")
        }
        if uid == "" {
            return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"success": false, "message": "login required"})
        }
        c.Locals(userKey, uid)
        return c.Next()
    }
}

func UserID(c *fiber.Ctx) string {
    if v := c.Locals(userKey); v != nil {
        if s, ok := v.(string); ok { return s }
    }
    return ""
}

