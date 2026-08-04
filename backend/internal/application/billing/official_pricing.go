package billing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

var (
	// ErrOfficialPricingProviderUnavailable 表示官方定价提供方未完成装配。
	ErrOfficialPricingProviderUnavailable = errors.New("official pricing provider unavailable")
	// ErrOfficialPricingEmpty 表示上游返回的官方定价目录没有有效模型。
	ErrOfficialPricingEmpty = errors.New("official pricing catalog is empty")
)

type openRouterPricingProvider interface {
	FetchModels(ctx context.Context) ([]byte, error)
}

// OfficialPricingItem 表示第三方官方模型定价项。
type OfficialPricingItem struct {
	ID            string
	CanonicalSlug string
	Name          string
	Pricing       OfficialUnitPricing
}

// OfficialUnitPricing 表示第三方官方价格字段。
type OfficialUnitPricing struct {
	Prompt          string
	Completion      string
	InputCacheRead  string
	InputCacheWrite string
}

type openRouterModelsResponse struct {
	Data []openRouterModelItem `json:"data"`
}

type openRouterModelItem struct {
	ID            string                 `json:"id"`
	CanonicalSlug string                 `json:"canonical_slug"`
	Name          string                 `json:"name"`
	Pricing       openRouterModelPricing `json:"pricing"`
}

type openRouterModelPricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
}

// SetOpenRouterPricingProvider 注入 OpenRouter 官方模型目录提供方。
func (s *Service) SetOpenRouterPricingProvider(provider openRouterPricingProvider) {
	if s == nil {
		return
	}
	s.openRouterPricingProvider = provider
}

// FetchOpenRouterOfficialPricing 获取并规范化 OpenRouter 官方模型定价。
func (s *Service) FetchOpenRouterOfficialPricing(ctx context.Context) ([]OfficialPricingItem, error) {
	if s == nil || s.openRouterPricingProvider == nil {
		return nil, ErrOfficialPricingProviderUnavailable
	}
	body, err := s.openRouterPricingProvider.FetchModels(ctx)
	if err != nil {
		return nil, err
	}
	var payload openRouterModelsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	items := make([]OfficialPricingItem, 0, len(payload.Data))
	for _, item := range payload.Data {
		normalized := normalizeOpenRouterOfficialPricingItem(item)
		if normalized.ID != "" {
			items = append(items, normalized)
		}
	}
	if len(items) == 0 {
		return nil, ErrOfficialPricingEmpty
	}
	return items, nil
}

func normalizeOpenRouterOfficialPricingItem(item openRouterModelItem) OfficialPricingItem {
	id := strings.TrimSpace(item.ID)
	if id == "" {
		return OfficialPricingItem{}
	}
	canonicalSlug := strings.TrimSpace(item.CanonicalSlug)
	if canonicalSlug == "" {
		canonicalSlug = id
	}
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = id
	}
	return OfficialPricingItem{
		ID:            id,
		CanonicalSlug: canonicalSlug,
		Name:          name,
		Pricing: OfficialUnitPricing{
			Prompt:          strings.TrimSpace(item.Pricing.Prompt),
			Completion:      strings.TrimSpace(item.Pricing.Completion),
			InputCacheRead:  strings.TrimSpace(item.Pricing.InputCacheRead),
			InputCacheWrite: strings.TrimSpace(item.Pricing.InputCacheWrite),
		},
	}
}
