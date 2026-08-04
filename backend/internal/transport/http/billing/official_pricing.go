package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

const (
	openRouterPricingCacheRelPath = "admin/openrouter-model-pricing.json"
	openRouterPricingCacheTTL     = 24 * time.Hour
)

type openRouterOfficialPricingCacheFile struct {
	FetchedAt time.Time                               `json:"fetchedAt"`
	Items     []OpenRouterOfficialPricingItemResponse `json:"items"`
}

// GetOpenRouterOfficialPricing godoc
// @Summary 管理员获取 OpenRouter 官方模型定价
// @Description 从 storage 缓存读取 OpenRouter 模型定价；缓存不存在、过期或 refresh=true 时由后端刷新。
// @Tags admin-billing
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param refresh query bool false "强制刷新缓存"
// @Success 200 {object} OpenRouterOfficialPricingResponseDoc
// @Failure 502 {object} ErrorDoc
// @Router /admin/billing/official-pricing/openrouter [get]
func (h *Handler) GetOpenRouterOfficialPricing(c *gin.Context) {
	refresh := strings.EqualFold(strings.TrimSpace(c.Query("refresh")), "true")
	cache, cacheOK := h.readOpenRouterOfficialPricingCache()
	respondWithStaleCache := func() {
		response.Success(c, OpenRouterOfficialPricingDataResponse{
			FetchedAt: cache.FetchedAt,
			Cached:    true,
			Stale:     true,
			Items:     cache.Items,
		})
	}
	if cacheOK && !refresh && !openRouterOfficialPricingCacheStale(cache.FetchedAt) {
		response.Success(c, OpenRouterOfficialPricingDataResponse{
			FetchedAt: cache.FetchedAt,
			Cached:    true,
			Stale:     false,
			Items:     cache.Items,
		})
		return
	}

	items, err := h.fetchOpenRouterOfficialPricing(c.Request.Context())
	if err != nil {
		if cacheOK {
			// 官方价格只用于管理员辅助填充；远程失败时优先保留可用旧缓存，避免阻塞已有配置流程。
			respondWithStaleCache()
			return
		}
		response.Error(c, http.StatusBadGateway, "fetch openrouter official pricing failed")
		return
	}
	if len(items) == 0 {
		if cacheOK {
			respondWithStaleCache()
			return
		}
		response.Error(c, http.StatusBadGateway, "openrouter official pricing is empty")
		return
	}

	nextCache := openRouterOfficialPricingCacheFile{
		FetchedAt: time.Now().UTC(),
		Items:     items,
	}
	if writeErr := h.writeOpenRouterOfficialPricingCache(nextCache); writeErr != nil {
		response.Error(c, http.StatusInternalServerError, "cache openrouter official pricing failed")
		return
	}
	response.Success(c, OpenRouterOfficialPricingDataResponse{
		FetchedAt: nextCache.FetchedAt,
		Cached:    false,
		Stale:     false,
		Items:     nextCache.Items,
	})
}

func (h *Handler) openRouterOfficialPricingCachePath() string {
	root := ""
	if h.cfg != nil {
		root = strings.TrimSpace(h.cfg.Snapshot().StorageRootDir)
	}
	if root == "" {
		root = "./storage"
	}
	return filepath.Join(root, filepath.FromSlash(openRouterPricingCacheRelPath))
}

func (h *Handler) readOpenRouterOfficialPricingCache() (openRouterOfficialPricingCacheFile, bool) {
	path := h.openRouterOfficialPricingCachePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return openRouterOfficialPricingCacheFile{}, false
	}
	var cache openRouterOfficialPricingCacheFile
	if err := json.Unmarshal(raw, &cache); err != nil || cache.FetchedAt.IsZero() || len(cache.Items) == 0 {
		return openRouterOfficialPricingCacheFile{}, false
	}
	return cache, true
}

func (h *Handler) writeOpenRouterOfficialPricingCache(cache openRouterOfficialPricingCacheFile) error {
	path := h.openRouterOfficialPricingCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (h *Handler) fetchOpenRouterOfficialPricing(ctx context.Context) ([]OpenRouterOfficialPricingItemResponse, error) {
	if h.service == nil {
		return nil, errors.New("openrouter pricing provider is not configured")
	}
	items, err := h.service.FetchOpenRouterOfficialPricing(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]OpenRouterOfficialPricingItemResponse, 0, len(items))
	for _, item := range items {
		result = append(result, OpenRouterOfficialPricingItemResponse{
			ID:            item.ID,
			CanonicalSlug: item.CanonicalSlug,
			Name:          item.Name,
			Pricing: OpenRouterOfficialPricingUnitPricingResponse{
				Prompt:          item.Pricing.Prompt,
				Completion:      item.Pricing.Completion,
				InputCacheRead:  item.Pricing.InputCacheRead,
				InputCacheWrite: item.Pricing.InputCacheWrite,
			},
		})
	}
	return result, nil
}

func openRouterOfficialPricingCacheStale(fetchedAt time.Time) bool {
	return fetchedAt.IsZero() || time.Since(fetchedAt) > openRouterPricingCacheTTL
}
