package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	dbpostgres "github.com/mesob-delivery/backend/internal/adapter/postgres"
	"github.com/mesob-delivery/backend/internal/auth/adapter/security"
)

const (
	adminEmail  = "hustlerw23@gmail.com"
	initialPass = "Pa$$word!"
	adminName   = "Admin"
	adminRole   = "admin"
	adminStatus = "active"
)

func main() {
	if err := seed(); err != nil {
		fmt.Fprintln(os.Stderr, "seed admin:", err)
		os.Exit(1)
	}
	fmt.Printf("Admin account %s is ready. Existing password hashes are preserved.\n", adminEmail)
}

func seed() error {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	phone := strings.TrimSpace(os.Getenv("ADMIN_PHONE"))
	if phone == "" {
		return fmt.Errorf("ADMIN_PHONE is required (users.phone is NOT NULL and UNIQUE)")
	}

	hash, err := (security.Argon2id{}).Hash(initialPass)
	if err != nil {
		return fmt.Errorf("hash initial password: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := dbpostgres.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	const query = `
		INSERT INTO public.users AS existing
			(name, phone, email, password_hash, role, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email) DO UPDATE SET
			password_hash = COALESCE(existing.password_hash, EXCLUDED.password_hash),
			role = EXCLUDED.role,
			status = EXCLUDED.status,
			updated_at = now()`
	if _, err := pool.Exec(ctx, query, adminName, phone, adminEmail, hash, adminRole, adminStatus); err != nil {
		return fmt.Errorf("upsert admin user: %w", err)
	}
	return nil
}
