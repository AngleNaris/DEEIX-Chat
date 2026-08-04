package billing

import (
	"context"
	"testing"
)

type officialPricingProviderStub struct {
	payload []byte
}

func (s officialPricingProviderStub) FetchModels(context.Context) ([]byte, error) {
	return s.payload, nil
}

func TestFetchOpenRouterOfficialPricingNormalizesCatalog(t *testing.T) {
	service := NewService(nil)
	service.SetOpenRouterPricingProvider(officialPricingProviderStub{payload: []byte(`{
		"data": [
			{
				"id": " openai/gpt-test ",
				"canonical_slug": "",
				"name": "",
				"pricing": {"prompt": " 0.1 ", "completion": " 0.2 "}
			},
			{"id": "   ", "name": "ignored"}
		]
	}`)})

	items, err := service.FetchOpenRouterOfficialPricing(context.Background())
	if err != nil {
		t.Fatalf("fetch official pricing: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one valid item, got %d", len(items))
	}
	item := items[0]
	if item.ID != "openai/gpt-test" || item.CanonicalSlug != item.ID || item.Name != item.ID {
		t.Fatalf("unexpected normalized identity: %#v", item)
	}
	if item.Pricing.Prompt != "0.1" || item.Pricing.Completion != "0.2" {
		t.Fatalf("unexpected normalized pricing: %#v", item.Pricing)
	}
}
