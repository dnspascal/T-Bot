package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/denismgaya/t-bot/internal/seed"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

//go:embed migrations
var migrationsFS embed.FS

func main() {
	_ = godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	migrateURL := strings.Replace(dbURL, "postgres://", "pgx5://", 1)

	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		log.Fatalf("migrate source: %v", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL)
	if err != nil {
		log.Fatalf("migrate.New: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migrate.Up: %v", err)
	}

	fmt.Println("migrations applied successfully")

	ctx := context.Background()
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("db connect for seed: %v", err)
	}
	defer db.Close()

	if err := seed.SeedSymbols(ctx, db); err != nil {
		log.Fatalf("seed: %v", err)
	}

	fmt.Println("seed applied successfully")
}
