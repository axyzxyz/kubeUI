package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/axyzxyz/kubeui/backend/internal/pkg/errcode"
	"github.com/axyzxyz/kubeui/backend/internal/store"
)

// Token 有效期约定(01-architecture §2)。
const (
	AccessTokenTTL  = 2 * time.Hour
	RefreshTokenTTL = 7 * 24 * time.Hour
	bcryptCost      = 10
)

// TokenPair 是登录/刷新后返回的凭证对。
type TokenPair struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"` // access token 过期时间
}

type claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// AuthService 处理登录、刷新、吊销与 access token 校验。
type AuthService struct {
	users   *store.UserRepo
	tokens  *store.RefreshTokenRepo
	secret  []byte
	nowFunc func() time.Time // 可注入,便于测试
}

// NewAuthService 构造 AuthService;secret 为 HS256 签名密钥。
func NewAuthService(users *store.UserRepo, tokens *store.RefreshTokenRepo, secret string) *AuthService {
	return &AuthService{
		users:   users,
		tokens:  tokens,
		secret:  []byte(secret),
		nowFunc: time.Now,
	}
}

// Login 校验用户名密码,签发 access/refresh token 并落库 refresh 哈希。
func (s *AuthService) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	u, err := s.users.GetUserByUsername(ctx, username)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.Unauthorized, "invalid username or password").
				WithCause(ErrInvalidCredentials)
		}
		return nil, err
	}
	if u.Disabled {
		return nil, errcode.New(errcode.UserDisabled, "user is disabled")
	}
	if err := comparePassword(u.PasswordHash, password); err != nil {
		return nil, err
	}
	return s.issuePair(ctx, u.ID, u.Username, u.Role)
}

// Refresh 轮转 refresh token 并签发新的凭证对;旧 refresh token 立即吊销。
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	hash := hashToken(refreshToken)
	rec, err := s.tokens.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.Unauthorized, "invalid refresh token")
		}
		return nil, err
	}
	if rec.Revoked || s.nowFunc().After(rec.ExpiresAt) {
		return nil, errcode.New(errcode.Unauthorized, "refresh token expired or revoked")
	}
	u, err := s.users.GetUserByID(ctx, rec.UserID)
	if err != nil {
		if isNotFound(err) {
			return nil, errcode.New(errcode.Unauthorized, "user no longer exists")
		}
		return nil, err
	}
	if u.Disabled {
		return nil, errcode.New(errcode.UserDisabled, "user is disabled")
	}
	if err := s.tokens.RevokeRefreshToken(ctx, hash); err != nil {
		return nil, err
	}
	return s.issuePair(ctx, u.ID, u.Username, u.Role)
}

// Revoke 吊销指定 refresh token(登出)。
func (s *AuthService) Revoke(ctx context.Context, refreshToken string) error {
	return s.tokens.RevokeRefreshToken(ctx, hashToken(refreshToken))
}

// RevokeAllForUser 吊销某用户全部 refresh token(禁用用户时调用)。
func (s *AuthService) RevokeAllForUser(ctx context.Context, userID int64) error {
	return s.tokens.RevokeAllForUser(ctx, userID)
}

// Identity 是 access token 解析出的身份。
type Identity struct {
	UserID   int64
	Username string
	Role     string
}

// VerifyAccessToken 校验 HS256 access token 并返回身份。
func (s *AuthService) VerifyAccessToken(ctx context.Context, token string) (Identity, error) {
	var c claims
	parsed, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil || !parsed.Valid {
		return Identity{}, errcode.New(errcode.Unauthorized, "invalid or expired token").WithCause(err)
	}
	return Identity{UserID: c.UserID, Username: c.Username, Role: c.Role}, nil
}

// HashPassword 生成 bcrypt 哈希。
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

// comparePassword 校验密码哈希,不匹配时返回 40100 业务错误。
func comparePassword(hash, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return errcode.New(errcode.Unauthorized, "invalid username or password").
			WithCause(ErrInvalidCredentials)
	}
	return nil
}

func (s *AuthService) issuePair(ctx context.Context, userID int64, username, role string) (*TokenPair, error) {
	now := s.nowFunc()
	c := claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refresh := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.tokens.CreateRefreshToken(ctx, &store.RefreshToken{
		UserID:    userID,
		TokenHash: hashToken(refresh),
		ExpiresAt: now.Add(RefreshTokenTTL),
	}); err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresAt: now.Add(AccessTokenTTL)}, nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}
