package config

import "os"

type Config struct {
    AppName        string
    Env            string
    Port           string
    DatabaseURL    string
    CoreAPIBase    string
    AllowedOrigins string
}

func getenv(k, def string) string {
    if v := os.Getenv(k); v != "" { return v }
    return def
}

func Load() Config {
    return Config{
        AppName:        getenv("APP_NAME", "berjis-logistics"),
        Env:            getenv("APP_ENV", "development"),
        Port:           getenv("PORT", "8081"),
        DatabaseURL:    getenv("DATABASE_URL", "root:password@tcp(localhost:3306)/berjis_logistics?parseTime=true"),
        CoreAPIBase:    getenv("CORE_API_BASE", "http://localhost:8080"),
        AllowedOrigins: getenv("ALLOWED_ORIGINS", "*"),
    }
}
