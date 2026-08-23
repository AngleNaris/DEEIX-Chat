package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

type routeSkillRepo struct {
	items []domainskill.Skill
}

func (r routeSkillRepo) ListSkills(context.Context, repository.SkillListFilter, int, int) ([]domainskill.Skill, int64, error) {
	return r.items, int64(len(r.items)), nil
}

func (r routeSkillRepo) GetSkill(_ context.Context, id uint) (*domainskill.Skill, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			item := r.items[index]
			return &item, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r routeSkillRepo) CreateSkill(context.Context, *domainskill.Skill) (*domainskill.Skill, error) {
	return nil, repository.ErrInvalidInput
}

func (r routeSkillRepo) PatchSkill(context.Context, uint, repository.SkillPatch) (*domainskill.Skill, error) {
	return nil, repository.ErrInvalidInput
}

func (r routeSkillRepo) DeleteSkill(context.Context, uint) (*domainskill.Skill, error) {
	return nil, repository.ErrInvalidInput
}

func TestSkillMineRouteIsNotCapturedByVisibleDetailRoute(t *testing.T) {
	router, _ := newRouteSkillTestRouter(routeSkillRepo{
		items: []domainskill.Skill{{
			ID:          1,
			Scope:       domainskill.ScopeUser,
			OwnerUserID: 7,
			Title:       "Review",
			Trigger:     "review",
			Markdown:    "private SKILL.md",
			Enabled:     true,
		}},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/skills/mine", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected /skills/mine to return 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "private SKILL.md") {
		t.Fatalf("expected mine route to return full skill payload, got %s", recorder.Body.String())
	}
}

func TestSkillPackageFileRouteReadsNestedTextPath(t *testing.T) {
	const (
		filePath = "scripts/nested/readme.md"
		content  = "nested text"
	)
	item := routePackageSkill()
	router, service := newRouteSkillTestRouter(routeSkillRepo{items: []domainskill.Skill{item}})
	store := objectstore.NewLocal(t.TempDir())
	key := "skills/builtin/1/versions/route-v1/" + filePath
	if _, err := store.Put(t.Context(), key, bytes.NewReader([]byte(content)), objectstore.PutOptions{
		SizeBytes: int64(len(content)),
	}); err != nil {
		t.Fatalf("seed package file: %v", err)
	}
	service.SetObjectStoreProvider(routeSkillStoreProvider{store: store})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/skills/1/package-file?path="+filePath, nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected package file route to return 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data SkillPackageFileDataResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode package file response: %v", err)
	}
	if response.Data.File.Path != filePath || response.Data.File.Content != content {
		t.Fatalf("unexpected package file response: %#v", response.Data.File)
	}
}

func TestSkillPackageFileRouteRequiresPath(t *testing.T) {
	router, _ := newRouteSkillTestRouter(routeSkillRepo{items: []domainskill.Skill{routePackageSkill()}})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/skills/1/package-file", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected missing path to return 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSkillPackageFileRouteRejectsBinaryFile(t *testing.T) {
	router, _ := newRouteSkillTestRouter(routeSkillRepo{items: []domainskill.Skill{routePackageSkill()}})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/skills/1/package-file?path=assets/icon.png", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected binary package file to return 415, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

type routeSkillStoreProvider struct {
	store objectstore.Store
}

func (p routeSkillStoreProvider) Open(context.Context) (objectstore.Store, error) {
	return p.store, nil
}

func newRouteSkillTestRouter(repo routeSkillRepo) (*gin.Engine, *appskill.Service) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ContextKeyUserID, uint(7))
		c.Next()
	})
	service := appskill.NewService(repo)
	NewModule(NewHandler(service)).RegisterRoutes(router.Group(""))
	return router, service
}

func routePackageSkill() domainskill.Skill {
	return domainskill.Skill{
		ID:                    1,
		Scope:                 domainskill.ScopeBuiltin,
		PackageType:           domainskill.PackageTypePackage,
		PackageStorageVersion: "route-v1",
		Enabled:               true,
		PackageFiles: []domainskill.PackageFile{
			{Path: "scripts/nested/readme.md", Kind: domainskill.FileKindText},
			{Path: "assets/icon.png", Kind: domainskill.FileKindBinary},
		},
	}
}
