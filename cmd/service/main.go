package main

import (
    "log"
    "os"

    "github.com/joho/godotenv"

    "github.com/berjistech/berjis-ecosystem/logistics/service/internal/config"
    "github.com/berjistech/berjis-ecosystem/logistics/service/internal/db"
    "github.com/berjistech/berjis-ecosystem/logistics/service/internal/server"
)

func main() {
    _ = godotenv.Load()
    cfg := config.Load()

    if _, err := db.Connect(cfg.DatabaseURL); err != nil {
        log.Printf("warn: failed to connect to logistics DB: %v", err)
    }

    app := server.New(server.Options{AllowedOrigins: cfg.AllowedOrigins})
    addr := ":" + cfg.Port
    log.Printf("starting %s on %s (env=%s)", cfg.AppName, addr, cfg.Env)
    if err := app.Listen(addr); err != nil {
        log.Println("shutdown:", err)
        os.Exit(1)
    }
}
