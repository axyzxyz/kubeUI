package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/axyzxyz/kubeui/backend/internal/store"
)

// newTestStore 打开内存 sqlite 并完成种子数据,供 service 层测试使用。
func newTestStore(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	ctx := context.Background()
	if err := store.SeedDefaults(ctx, db, BuiltinRoleSeeds()); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	return db
}

func newTestAuth(t *testing.T) (*AuthService, *store.UserRepo) {
	t.Helper()
	db := newTestStore(t)
	users := store.NewUserRepo(db)
	tokens := store.NewRefreshTokenRepo(db)
	return NewAuthService(users, tokens, "test-secret"), users
}

func seedUser(t *testing.T, users *store.UserRepo, username, password, role string) {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := users.CreateUser(context.Background(), &store.User{
		Username: username, PasswordHash: hash, Role: role,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func TestAuthServiceLogin(t *testing.T) {
	auth, users := newTestAuth(t)
	seedUser(t, users, "alice", "password123", "admin")

	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{name: "ok", username: "alice", password: "password123"},
		{name: "wrong password", username: "alice", password: "nope", wantErr: true},
		{name: "unknown user", username: "bob", password: "password123", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair, err := auth.Login(context.Background(), tt.username, tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if pair.AccessToken == "" || pair.RefreshToken == "" {
					t.Fatal("token pair must be non-empty")
				}
				if !pair.ExpiresAt.After(time.Now()) {
					t.Fatal("access token must not be expired immediately")
				}
			}
		})
	}
}

func TestAuthServiceLoginDisabledUser(t *testing.T) {
	auth, users := newTestAuth(t)
	seedUser(t, users, "carol", "password123", "viewer")
	u, _ := users.GetUserByUsername(context.Background(), "carol")
	u.Disabled = true
	if err := users.UpdateUser(context.Background(), u); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if _, err := auth.Login(context.Background(), "carol", "password123"); err == nil {
		t.Fatal("disabled user login must fail")
	}
}

func TestAuthServiceRefreshRotation(t *testing.T) {
	auth, users := newTestAuth(t)
	seedUser(t, users, "dave", "password123", "operator")
	ctx := context.Background()

	pair, err := auth.Login(ctx, "dave", "password123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	pair2, err := auth.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if pair2.RefreshToken == pair.RefreshToken {
		t.Fatal("refresh token must rotate on every refresh")
	}
	// 旧 refresh token 已被吊销,再次使用必须失败。
	if _, err := auth.Refresh(ctx, pair.RefreshToken); err == nil {
		t.Fatal("reused refresh token must be rejected")
	}
	ident, err := auth.VerifyAccessToken(ctx, pair2.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if ident.Username != "dave" || ident.Role != "operator" || ident.UserID == 0 {
		t.Fatalf("unexpected identity: %+v", ident)
	}
}

func TestAuthServiceRevoke(t *testing.T) {
	auth, users := newTestAuth(t)
	seedUser(t, users, "erin", "password123", "viewer")
	ctx := context.Background()

	pair, err := auth.Login(ctx, "erin", "password123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := auth.Revoke(ctx, pair.RefreshToken); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := auth.Refresh(ctx, pair.RefreshToken); err == nil {
		t.Fatal("revoked refresh token must be rejected")
	}
}

func TestAuthServiceVerifyAccessTokenRejectsGarbage(t *testing.T) {
	auth, _ := newTestAuth(t)
	if _, err := auth.VerifyAccessToken(context.Background(), "not-a-jwt"); err == nil {
		t.Fatal("garbage token must be rejected")
	}
}
