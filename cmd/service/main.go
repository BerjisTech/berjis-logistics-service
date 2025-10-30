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
		dir := os.Getenv("MIGRATIONS_DIR")
		if dir == "" {
			dir = "./migrations"
		}
		runner := migrate.Runner{Dir: dir}
		if err := runner.Up(conn); err != nil {
			log.Printf("warn: migrations failed: %v", err)
		}
	}

	app := server.New(server.Options{AllowedOrigins: cfg.AllowedOrigins, DB: conn, Env: cfg.Env, AuthHS256: cfg.AuthHS256Secret})
	addr := ":" + cfg.Port
	log.Printf("starting %s on %s (env=%s)", cfg.AppName, addr, cfg.Env)
	if err := app.Listen(addr); err != nil {
		log.Println("shutdown:", err)
		os.Exit(1)
	}
}
