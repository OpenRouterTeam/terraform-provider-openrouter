package hooks

import (
	"net/http"

	"github.com/OpenRouterTeam/terraform-provider-openrouter/internal/explicitnull"
)

/*
 * This file is only ever generated once on the first generation and then is free to be modified.
 * Any hooks you wish to add should be registered in the initHooks function. Feel free to define
 * your hooks in this file or in separate files in the hooks package.
 *
 * Hooks are registered per SDK instance, and are valid for the lifetime of the SDK instance.
 */

func initHooks(h *Hooks) {
	// Hooks are registered per SDK instance, and are valid for the lifetime of the SDK instance.
	// Add any hooks you wish to add here. Feel free to define your hooks in this file or in
	// separate files in the hooks package.
	//
	// The following methods are available for registering hooks:
	_ = h.registerSDKInitHook
	_ = h.registerBeforeRequestHook
	_ = h.registerAfterSuccessHook
	_ = h.registerAfterErrorHook

	// Example:
	// exampleHook := &ExampleHook{}
	// h.registerSDKInitHook(exampleHook)
	// h.registerBeforeRequestHook(exampleHook)
	// h.registerAfterErrorHook(exampleHook)
	// h.registerAfterSuccessHook(exampleHook)

	h.registerBeforeRequestHook(explicitNullHook{})
	h.registerBeforeRequestHook(privateProviderExplicitNullsHook{})
}

// explicitNullHook sends the explicit JSON nulls requested through
// explicitnull.WithFields, which the generated request models cannot express.
type explicitNullHook struct{}

func (explicitNullHook) BeforeRequest(_ BeforeRequestContext, req *http.Request) (*http.Request, error) {
	return explicitnull.Apply(req)
}
