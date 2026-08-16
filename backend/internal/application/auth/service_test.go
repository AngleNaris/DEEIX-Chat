package auth

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	domainuser "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/user"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/requestmeta"
)

func TestDeleteAccountDoesNotWritePostDeleteAuthEvents(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour)
	code := "123456"
	token := "delete-account-token"
	repo := &deleteAccountRepo{
		securityVerificationRepo: securityVerificationRepo{
			user: &domainuser.User{
				ID:              7,
				PublicID:        "user_public",
				Username:        "delete-user",
				Email:           "delete@example.com",
				Role:            domainuser.RoleUser,
				EmailVerifiedAt: &now,
			},
			pendingVerifications: []domainuser.ContactVerification{{
				ID:        1,
				UserID:    7,
				Channel:   domainuser.ContactVerificationChannelEmail,
				Purpose:   domainuser.ContactVerificationPurposeAccountDelete,
				Target:    "delete@example.com",
				Token:     token,
				CodeHash:  hashRegistrationCode("test-secret", token, code),
				Status:    domainuser.ContactVerificationStatusPending,
				ExpiresAt: &expiresAt,
			}},
		},
		storagePaths: []string{"object-a", "object-b"},
	}
	store := &deleteAccountStore{deleteErrors: map[string]error{"object-b": errors.New("delete failed")}}
	service := newTestService(config.Config{JWTSecret: "test-secret", EmailVerificationEnabled: true}, repo, nil)
	service.SetObjectStoreProvider(deleteAccountStoreProvider{store: store})

	err := service.DeleteAccount(context.Background(), 7, string(SecurityVerificationMethodEmail), code, "request", requestmeta.SessionAuditContext{})
	if err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if !repo.deleted || repo.deletedUserID != 7 {
		t.Fatalf("account deletion state = deleted:%v user:%d", repo.deleted, repo.deletedUserID)
	}
	if repo.authEventCount != 0 {
		t.Fatalf("post-delete auth event count = %d, want 0", repo.authEventCount)
	}
	if len(store.deleted) != 2 || store.deleted[0] != "object-a" || store.deleted[1] != "object-b" {
		t.Fatalf("deleted object paths = %v", store.deleted)
	}
}

type deleteAccountRepo struct {
	securityVerificationRepo
	storagePaths   []string
	deleted        bool
	deletedUserID  uint
	authEventCount int
}

func (r *deleteAccountRepo) DeleteAccountHardWithStoragePaths(_ context.Context, userID uint) ([]string, error) {
	r.deleted = true
	r.deletedUserID = userID
	return append([]string(nil), r.storagePaths...), nil
}

func (r *deleteAccountRepo) DeleteAccountHard(_ context.Context, userID uint) error {
	r.deleted = true
	r.deletedUserID = userID
	return nil
}

func (r *deleteAccountRepo) RecordAuthEvent(context.Context, uint, string, string, string, string, string, string, string) error {
	r.authEventCount++
	return nil
}

type deleteAccountStoreProvider struct {
	store objectstore.Store
}

func (p deleteAccountStoreProvider) Open(context.Context) (objectstore.Store, error) {
	return p.store, nil
}

type deleteAccountStore struct {
	deleted      []string
	deleteErrors map[string]error
}

func (*deleteAccountStore) Put(context.Context, string, io.Reader, objectstore.PutOptions) (objectstore.ObjectInfo, error) {
	return objectstore.ObjectInfo{}, errors.New("not implemented")
}

func (*deleteAccountStore) Open(context.Context, string) (io.ReadCloser, objectstore.ObjectInfo, error) {
	return nil, objectstore.ObjectInfo{}, errors.New("not implemented")
}

func (s *deleteAccountStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.deleteErrors[key]
}

func (*deleteAccountStore) Materialize(context.Context, string) (string, func(), error) {
	return "", nil, errors.New("not implemented")
}

type validateAccessSessionRepo struct {
	repository.AuthRepository
	session *domainuser.Session
}

func (r *validateAccessSessionRepo) GetSessionByUserAndSessionID(_ context.Context, userID uint, sessionID string) (*domainuser.Session, error) {
	if r.session == nil || r.session.UserID != userID || r.session.SessionID != sessionID {
		return nil, repository.ErrNotFound
	}
	return r.session, nil
}

func (r *validateAccessSessionRepo) TouchSessionActivity(_ context.Context, _ uint, _ string, _ repository.UpdateSessionActivityInput) error {
	return nil
}

func TestNormalizeAppearancePreferencesAllowsFontSize(t *testing.T) {
	for _, fontSize := range []string{"small", "standard", "medium", "large"} {
		payload := `{"theme":"system","preset":"default","chatFont":"default","chatFontWeight":"regular","fontSize":"` + fontSize + `"}`

		if _, err := normalizeAppearancePreferences(payload); err != nil {
			t.Fatalf("expected fontSize %q appearance preference to be valid, got %v", fontSize, err)
		}
	}
}

func TestNormalizeAppearancePreferencesDefaultsInvalidFontSize(t *testing.T) {
	payload := `{"fontSize":"huge"}`

	normalized, err := normalizeAppearancePreferences(payload)
	if err != nil {
		t.Fatalf("expected invalid fontSize appearance preference to fall back, got %v", err)
	}
	if normalized != `{"fontSize":"standard"}` {
		t.Fatalf("expected invalid fontSize to fall back to standard, got %s", normalized)
	}
}

func TestNormalizeAppearancePreferencesRejectsUnknownKey(t *testing.T) {
	payload := `{"fontSize":"standard","unknown":"value"}`

	if _, err := normalizeAppearancePreferences(payload); err == nil {
		t.Fatal("expected unknown appearance preference key to be rejected")
	}
}

func TestShouldRequireInitialUsernameOnlyForBootstrapSuperAdmin(t *testing.T) {
	if !shouldRequireInitialUsername(domainuser.User{
		Username: "admin",
		Role:     domainuser.RoleSuperAdmin,
	}, "admin") {
		t.Fatal("expected bootstrap superadmin username to require initialization change")
	}

	if shouldRequireInitialUsername(domainuser.User{
		Username:          "admin",
		Role:              domainuser.RoleSuperAdmin,
		UsernameChangedAt: ptrTime(time.Now()),
	}, "admin") {
		t.Fatal("expected changed superadmin username to remain optional")
	}

	if shouldRequireInitialUsername(domainuser.User{
		Username:    "user",
		Role:        domainuser.RoleUser,
		EmailSource: domainuser.EmailSourceLocalRegister,
	}, "admin") {
		t.Fatal("expected local registration user username to remain optional")
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func TestValidateAccessSessionAllowsTokenIssuedBeforeLatestRefresh(t *testing.T) {
	now := time.Now()
	createdAt := now.Add(-30 * time.Minute)
	lastSeenAt := now
	service := &Service{
		repo: &validateAccessSessionRepo{
			session: &domainuser.Session{
				SessionID:  "session-id",
				UserID:     1,
				AccessJTI:  "latest-access-jti",
				CreatedAt:  createdAt,
				IssuedAt:   now,
				LastSeenAt: &lastSeenAt,
				ExpiresAt:  now.Add(24 * time.Hour),
			},
		},
	}

	err := service.ValidateAccessSession(
		context.Background(),
		1,
		"session-id",
		createdAt.Add(5*time.Minute),
		requestmeta.SessionAuditContext{},
	)
	if err != nil {
		t.Fatalf("expected access token issued before latest refresh to remain valid, got %v", err)
	}
}

func TestValidateAccessSessionRejectsTokenBeforeSessionCreation(t *testing.T) {
	now := time.Now()
	createdAt := now.Add(-30 * time.Minute)
	lastSeenAt := now
	service := &Service{
		repo: &validateAccessSessionRepo{
			session: &domainuser.Session{
				SessionID:  "session-id",
				UserID:     1,
				CreatedAt:  createdAt,
				LastSeenAt: &lastSeenAt,
				ExpiresAt:  now.Add(24 * time.Hour),
			},
		},
	}

	err := service.ValidateAccessSession(
		context.Background(),
		1,
		"session-id",
		createdAt.Add(-accessTokenSessionClockSkew-time.Second),
		requestmeta.SessionAuditContext{},
	)
	if err == nil {
		t.Fatal("expected access token issued before session creation to be rejected")
	}
}
