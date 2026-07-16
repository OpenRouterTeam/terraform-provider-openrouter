# openrouter

Terraform Provider for the *openrouter* API.

[![Built by Speakeasy](https://img.shields.io/badge/Built_by-SPEAKEASY-374151?style=for-the-badge&labelColor=f3f4f6)](https://www.speakeasy.com/?utm_source=openrouter&utm_campaign=terraform)
[![License: MIT](https://img.shields.io/badge/LICENSE_//_MIT-3b5bdb?style=for-the-badge&labelColor=eff6ff)](https://opensource.org/licenses/MIT)


## 🏗 **Welcome to your new Terraform Provider!** 🏗

It has been generated successfully based on your OpenAPI spec. However, it is not yet ready for production use. Here are some next steps:
- [ ] 🛠 Add resources and datasources to your SDK by [annotating your OAS](https://www.speakeasy.com/docs/customize-terraform/terraform-extensions#map-api-entities-to-terraform-resources)
- [ ] ♻️ Refine your terraform provider quickly by iterating locally with the [Speakeasy CLI](https://github.com/speakeasy-api/speakeasy)
- [ ] 🎁 Publish your terraform provider to hashicorp registry by [configuring automatic publishing](https://www.speakeasy.com/docs/terraform-publishing)
- [ ] ✨ When ready to productionize, delete this section from the README

<!-- Start Summary [summary] -->
## Summary

OpenRouter API: OpenAI-compatible API with additional OpenRouter features

For more information about the API: [OpenRouter Documentation](https://openrouter.ai/docs)
<!-- End Summary [summary] -->

<!-- Start Table of Contents [toc] -->
## Table of Contents
<!-- $toc-max-depth=2 -->
* [openrouter](#openrouter)
  * [🏗 **Welcome to your new Terraform Provider!** 🏗](#welcome-to-your-new-terraform-provider)
  * [Installation](#installation)
  * [Authentication](#authentication)
  * [Available Resources and Data Sources](#available-resources-and-data-sources)
  * [Testing the provider locally](#testing-the-provider-locally)
* [Development](#development)
  * [Contributions](#contributions)

<!-- End Table of Contents [toc] -->

<!-- Start Installation [installation] -->
## Installation

To install this provider, copy and paste this code into your Terraform configuration. Then, run `terraform init`.

```hcl
terraform {
  required_providers {
    openrouter = {
      source  = "speakeasy/openrouter"
      version = "0.0.7"
    }
  }
}

provider "openrouter" {
  server_url = "..." # Optional
}
```
<!-- End Installation [installation] -->

<!-- Start Authentication [security] -->
## Authentication

This provider supports authentication configuration via provider configuration.

Available configuration:

| Provider Attribute | Description |
|---|---|
| `api_key` | API key as bearer token in Authorization header. |
<!-- End Authentication [security] -->

<!-- Start Available Resources and Data Sources [operations] -->
## Available Resources and Data Sources

### Managed Resources

* [openrouter_api_key](docs/resources/api_key.md)
* [openrouter_byok_key](docs/resources/byok_key.md)
* [openrouter_guardrail](docs/resources/guardrail.md)
* [openrouter_observability_destination](docs/resources/observability_destination.md)
* [openrouter_workspace](docs/resources/workspace.md)

### Data Sources

* [openrouter_api_key](docs/data-sources/api_key.md)
* [openrouter_api_keys](docs/data-sources/api_keys.md)
* [openrouter_byok_key](docs/data-sources/byok_key.md)
* [openrouter_byok_keys](docs/data-sources/byok_keys.md)
* [openrouter_credits](docs/data-sources/credits.md)
* [openrouter_guardrail](docs/data-sources/guardrail.md)
* [openrouter_guardrail_key_assignments](docs/data-sources/guardrail_key_assignments.md)
* [openrouter_guardrail_member_assignments](docs/data-sources/guardrail_member_assignments.md)
* [openrouter_guardrails](docs/data-sources/guardrails.md)
* [openrouter_model](docs/data-sources/model.md)
* [openrouter_models](docs/data-sources/models.md)
* [openrouter_observability_destination](docs/data-sources/observability_destination.md)
* [openrouter_observability_destinations](docs/data-sources/observability_destinations.md)
* [openrouter_preset](docs/data-sources/preset.md)
* [openrouter_presets](docs/data-sources/presets.md)
* [openrouter_providers](docs/data-sources/providers.md)
* [openrouter_workspace](docs/data-sources/workspace.md)
* [openrouter_workspace_budgets](docs/data-sources/workspace_budgets.md)
* [openrouter_workspace_members](docs/data-sources/workspace_members.md)
* [openrouter_workspaces](docs/data-sources/workspaces.md)
<!-- End Available Resources and Data Sources [operations] -->

<!-- Start Testing the provider locally [usage] -->
## Testing the provider locally

#### Local Provider

Should you want to validate a change locally, the `--debug` flag allows you to execute the provider against a terraform instance locally.

This also allows for debuggers (e.g. delve) to be attached to the provider.

```sh
go run main.go --debug
# Copy the TF_REATTACH_PROVIDERS env var
# In a new terminal
cd examples/your-example
TF_REATTACH_PROVIDERS=... terraform init
TF_REATTACH_PROVIDERS=... terraform apply
```

#### Compiled Provider

Terraform allows you to use local provider builds by setting a `dev_overrides` block in a configuration file called `.terraformrc`. This block overrides all other configured installation methods.

1. Execute `go build` to construct a binary called `terraform-provider-openrouter`
2. Ensure that the `.terraformrc` file is configured with a `dev_overrides` section such that your local copy of terraform can see the provider binary

Terraform searches for the `.terraformrc` file in your home directory and applies any configuration settings you set.

```
provider_installation {

  dev_overrides {
      "registry.terraform.io/openrouter/openrouter" = "<PATH>"
  }

  # For all other providers, install them directly from their origin provider
  # registries as normal. If you omit this, Terraform will _only_ use
  # the dev_overrides block, and so no other providers will be available.
  direct {}
}
```
<!-- End Testing the provider locally [usage] -->

<!-- Placeholder for Future Speakeasy SDK Sections -->

# Development

## Contributions

While we value open-source contributions to this terraform provider, this library is generated programmatically. Any manual changes added to internal files will be overwritten on the next generation.
We look forward to hearing your feedback. Feel free to open a PR or an issue with a proof of concept and we'll do our best to include it in a future release. 

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=openrouter&utm_campaign=terraform)
