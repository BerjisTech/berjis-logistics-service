package main

import (
    "log"
    "os"

    "github.com/joho/godotenv"

    "github.com/berjistech/berjis-ecosystem/logistics/service/internal/config"
    "github.com/berjistech/berjis-ecosystem/logistics/service/internal/db"
    "github.com/berjistech/berjis-ecosystem/logistics/service/internal/migrate"
    "github.com/berjistech/berjis-ecosystem/logistics/service/internal/server"
)

func main() {
    _ = godotenv.Load()
    cfg := config.Load()

    conn, err := db.Connect(cfg.DatabaseURL)
    if err != nil {
        log.Printf("warn: failed to connect to logistics DB: %v", err)
    } else {
        runner := migrate.Runner{Dir: "./migrations"}
        if err := runner.Up(conn); err != nil {
            log.Printf("warn: migrations failed: %v", err)
        }
    }

    app := server.New(server.Options{AllowedOrigins: cfg.AllowedOrigins, DB: conn})
    addr := ":" + cfg.Port
    log.Printf("starting %s on %s (env=%s)", cfg.AppName, addr, cfg.Env)
    if err := app.Listen(addr); err != nil {
        log.Println("shutdown:", err)
        os.Exit(1)
    }
}
