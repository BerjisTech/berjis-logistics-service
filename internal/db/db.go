package db

import (
    "github.com/jmoiron/sqlx"
    _ "github.com/go-sql-driver/mysql"
)

func Connect(dsn string) (*sqlx.DB, error) {
    return sqlx.Connect("mysql", dsn)
}
