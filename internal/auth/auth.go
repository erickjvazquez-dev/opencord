// Package auth handles local accounts: bcrypt password hashing, JWT issuing and
// verification, and the HTTP middleware that gates protected routes.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserExists   = errors.New("username already taken")
	ErrInvalidLogin = errors.New("invalid username or password")
)

// User is the public, safe-to-serialize view of an account.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Claims is the JWT payload. Username is embedded so the WebSocket layer can
// label messages without a DB round-trip.
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type Service struct {
	pool   *pgxpool.Pool
	secret []byte
	ttl    time.Duration
}

func New(pool *pgxpool.Pool, secret []byte, ttl time.Duration) *Service {
	return &Service{pool: pool, secret: secret, ttl: ttl}
}

func (s *Service) Register(ctx context.Context, username, password string) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u := User{Username: username}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, string(hash),
	).Scan(&u.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return User{}, ErrUserExists
		}
		return User{}, err
	}
	return u, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (User, error) {
	var u User
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, password_hash FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrInvalidLogin
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, ErrInvalidLogin
	}
	return u, nil
}

// Issue mints a signed JWT for the user.
func (s *Service) Issue(u User) (string, error) {
	claims := Claims{
		Username: u.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// Parse verifies a token and returns the embedded user.
func (s *Service) Parse(token string) (User, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return User{}, err
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: claims.Username}, nil
}

type ctxKey int

const userKey ctxKey = 0

// Middleware rejects requests without a valid token and stashes the user in the
// request context for downstream handlers.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Parse(TokenFromRequest(r))
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid or missing token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

// UserFrom extracts the authenticated user placed by Middleware.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey).(User)
	return u, ok
}

// TokenFromRequest reads the bearer token from the Authorization header, or
// from a ?token= query param (browsers can't set headers on a WebSocket).
func TokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("token")
}
