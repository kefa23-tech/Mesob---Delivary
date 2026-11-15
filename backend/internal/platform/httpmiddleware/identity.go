package httpmiddleware

import (
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// AccessTokenIdentity validates the signed access token and sets the identity
// header consumed by the existing handlers. A supplied X-User-ID is ignored.
func AccessTokenIdentity(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(parts[1], claims, func(token *jwt.Token) (any, error) {
				if token.Method != jwt.SigningMethodHS256 {
					return nil, jwt.ErrTokenSignatureInvalid
				}
				return []byte(secret), nil
			})
			if err != nil || token == nil || !token.Valid {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			subject, err := claims.GetSubject()
			if err != nil || strings.TrimSpace(subject) == "" {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			request := r.Clone(r.Context())
			request.Header = r.Header.Clone()
			request.Header.Set("X-User-ID", subject)
			next.ServeHTTP(w, request)
		})
	}
}
