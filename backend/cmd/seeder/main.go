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
	adminName   = "Admin"
	adminRole   = "admin"
	adminStatus = "active"
)

func main() {
	if err := seed(); err != nil {
		fmt.Fprintln(os.Stderr, "seed admin:", err)
		os.Exit(1)
	}
	if reset, _ := resetPasswordEnabled(); reset {
		fmt.Println("Admin account password reset and verified.")
		return
	}
	fmt.Println("Admin account is ready. Existing password hashes are preserved.")
}

func seed() error {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	adminEmail := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
	if adminEmail == "" {
		return fmt.Errorf("ADMIN_EMAIL is required")
	}
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if strings.TrimSpace(adminPassword) == "" {
		return fmt.Errorf("ADMIN_PASSWORD is required")
	}
	phone := strings.TrimSpace(os.Getenv("ADMIN_PHONE"))
	if phone == "" {
		return fmt.Errorf("ADMIN_PHONE is required (users.phone is NOT NULL and UNIQUE)")
	}
	resetPassword, err := resetPasswordEnabled()
	if err != nil {
		return err
	}

	hasher := security.Argon2id{}
	hash, err := hasher.Hash(adminPassword)
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

	const preserveQuery = `
		INSERT INTO public.users AS existing
			(name, phone, email, password_hash, role, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			updated_at = now()
		WHERE existing.password_hash IS NULL
			AND existing.role = 'admin'
			AND existing.status = 'active'`
	const resetQuery = `
		INSERT INTO public.users AS existing
			(name, phone, email, password_hash, role, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			updated_at = now()
		WHERE existing.role = 'admin'
			AND existing.status = 'active'`
	query := preserveQuery
	if resetPassword {
		query = resetQuery
	}
	tag, err := pool.Exec(ctx, query, adminName, phone, adminEmail, hash, adminRole, adminStatus)
	if err != nil {
		return fmt.Errorf("upsert admin user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var activeAdminExists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.users WHERE email = $1 AND role = 'admin' AND status = 'active')`, adminEmail).Scan(&activeAdminExists); err != nil {
			return fmt.Errorf("verify existing admin user: %w", err)
		}
		if !activeAdminExists {
			return fmt.Errorf("email already belongs to a user who is not an active admin; existing account was left unchanged")
		}
	}
	if resetPassword {
		var role, status, storedHash string
		if err := pool.QueryRow(ctx, `SELECT role, status, COALESCE(password_hash, '') FROM public.users WHERE email = $1`, adminEmail).Scan(&role, &status, &storedHash); err != nil {
			return fmt.Errorf("verify reset admin account: %w", err)
		}
		if role != adminRole || status != adminStatus {
			return fmt.Errorf("password reset target is not an active admin")
		}
		if !strings.HasPrefix(storedHash, "$argon2id$") {
			return fmt.Errorf("stored password hash is not Argon2id")
		}
		if err := hasher.Compare(storedHash, adminPassword); err != nil {
			return fmt.Errorf("verify stored Argon2id password hash: %w", err)
		}
	}
	return nil
}

func resetPasswordEnabled() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_RESET_PASSWORD"))) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("ADMIN_RESET_PASSWORD must be true or false")
	}
}
