package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk"
	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/sdk/models/operations"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Guardrail allowed_models and ignored_models accept a model slug or its
// canonical slug, but the API stores and returns canonical slugs, in request
// order with duplicates removed (openrouter-web
// packages/guardrails/helpers/resolve-model-identifiers.ts). Create and Update
// keep the planned list, but Read used to copy the canonical list into state,
// so a config naming e.g. "deepseek/deepseek-v4-flash-0731" planned a change
// on every run (#347). Read now keeps the prior list when the response is
// exactly what the API makes of it, and stores the response otherwise
// (out-of-band changes, import).

// keepConfiguredModelIDs restores prior's allowed_models and ignored_models on
// data when the refreshed lists are the API's canonical form of them.
func (r *GuardrailResource) keepConfiguredModelIDs(ctx context.Context, prior, data *GuardrailResourceModel) diag.Diagnostics {
	var canonical map[string]string
	lookup := func() map[string]string {
		if canonical == nil {
			var err error
			if canonical, err = canonicalModelSlugs(ctx, r.client); err != nil {
				tflog.Warn(ctx, "could not list models to match guardrail model ids to their canonical slugs; storing the API's values", map[string]interface{}{"error": err.Error()})
				canonical = map[string]string{}
			}
		}
		return canonical
	}
	data.AllowedModels = keepEquivalentModelIDs(prior.AllowedModels, data.AllowedModels, lookup)
	data.IgnoredModels = keepEquivalentModelIDs(prior.IgnoredModels, data.IgnoredModels, lookup)
	return nil
}

// keepEquivalentModelIDs returns prior when got is the API's resolution of
// prior: each prior id replaced by itself or its canonical slug, in order,
// keeping the first of any duplicates. Otherwise it returns got. lookup is
// called only when the lists differ.
func keepEquivalentModelIDs(prior, got []types.String, lookup func() map[string]string) []types.String {
	if len(prior) == 0 || got == nil {
		return got
	}
	if equalStringLists(prior, got) {
		return got
	}
	canonical := lookup()

	seen := map[string]bool{}
	i := 0
	for _, p := range prior {
		if p.IsNull() || p.IsUnknown() {
			return got
		}
		forms := modelIDForms(p.ValueString(), canonical)
		if slices.ContainsFunc(forms, func(f string) bool { return seen[f] }) {
			// The API dropped this one as a duplicate of an earlier entry.
			continue
		}
		if i >= len(got) || !slices.Contains(forms, got[i].ValueString()) {
			return got
		}
		seen[got[i].ValueString()] = true
		i++
	}
	if i != len(got) {
		return got
	}
	return prior
}

// modelIDForms lists the values the API may store for id: id itself, its
// canonical slug and, for a variant such as "author/model:free", the
// canonical slug with the variant suffix. The guardrail resolver stores a
// variant id as its permaslug plus the variant
// (openrouter-web packages/routing/endpoints/constructor.ts,
// packages/models/variants/shared.ts), while /models reports canonical_slug
// as the permaslug alone, so "x/y:free" is stored as "<canonical_slug>:free".
func modelIDForms(id string, canonical map[string]string) []string {
	forms := []string{id}
	c, ok := canonical[id]
	if !ok || c == "" || c == id {
		return forms
	}
	forms = append(forms, c)
	if _, variant, found := strings.Cut(id, ":"); found && !strings.Contains(c, ":") {
		forms = append(forms, c+":"+variant)
	}
	return forms
}

func equalStringLists(a, b []types.String) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

// modelSlugCaches holds one model id -> canonical slug map per configured
// client, fetched at most once per provider run.
var modelSlugCaches sync.Map // *sdk.OpenRouter -> *modelSlugCache

type modelSlugCache struct {
	once      sync.Once
	canonical map[string]string
	err       error
}

func canonicalModelSlugs(ctx context.Context, client *sdk.OpenRouter) (map[string]string, error) {
	v, _ := modelSlugCaches.LoadOrStore(client, &modelSlugCache{})
	c := v.(*modelSlugCache)
	c.once.Do(func() {
		c.canonical, c.err = fetchCanonicalModelSlugs(ctx, client)
	})
	return c.canonical, c.err
}

// fetchCanonicalModelSlugs pages through GET /models. output_modalities=all
// matters: the default lists only text-output models. The request is sent
// without credentials: since openrouter-web #47740, a credentialed caller
// whose account has the filtered model catalog enabled gets a list filtered
// by its provider preferences and guardrails, which can omit the very models
// a guardrail names (services/cfw-public-api/src/helpers/filtered-model-catalog.ts).
func fetchCanonicalModelSlugs(ctx context.Context, client *sdk.OpenRouter) (map[string]string, error) {
	all := "all"
	limit := int64(500)
	canonical := map[string]string{}
	for offset := int64(0); ; {
		res, err := client.Models.List(ctx, operations.GetModelsRequest{
			OutputModalities: &all,
			Offset:           &offset,
			Limit:            &limit,
		}, operations.WithSetHeaders(map[string]string{"Authorization": ""}))
		if err != nil {
			return nil, err
		}
		if res.ModelsListResponse == nil {
			return nil, fmt.Errorf("listing models: unexpected HTTP %d", res.StatusCode)
		}
		page := res.ModelsListResponse.Data
		for _, m := range page {
			canonical[m.ID] = m.CanonicalSlug
		}
		offset += int64(len(page))
		if len(page) == 0 || offset >= res.ModelsListResponse.TotalCount {
			return canonical, nil
		}
	}
}
