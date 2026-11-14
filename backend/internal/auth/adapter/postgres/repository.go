package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mesob-delivery/backend/internal/auth/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func NewUserRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }
func (r *Repository) FindByPhone(ctx context.Context, phone string) (domain.User, error) {
	return r.find(ctx, "phone", phone)
}
func (r *Repository) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	return r.find(ctx, "email", email)
}
func (r *Repository) find(ctx context.Context, column, value string) (domain.User, error) {
	query := fmt.Sprintf("SELECT id::text, name, phone, COALESCE(email, ''), COALESCE(password_hash, ''), role FROM users WHERE %s = $1 AND status = 'active'", column)
	var user domain.User
	var role string
	err := r.pool.QueryRow(ctx, query, value).Scan(&user.ID, &user.Name, &user.Phone, &user.Email, &user.PasswordHash, &role)
	user.Role = domain.Role(role)
	return user, err
}
func (r *Repository) Create(ctx context.Context, user domain.User) (domain.User, error) {
	err := r.pool.QueryRow(ctx, `INSERT INTO users (name, phone, email, password_hash, role, status) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, 'active') RETURNING id::text`, user.Name, user.Phone, user.Email, user.PasswordHash, user.Role).Scan(&user.ID)
	return user, err
}
