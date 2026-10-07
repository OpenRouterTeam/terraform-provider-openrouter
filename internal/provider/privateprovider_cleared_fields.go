package provider

// privateProviderClearedFields lists the nullable request fields that are null
// in data, for explicitnull to send. The generated request models omit nil
// fields, the API keeps any field a PATCH leaves out, and create requires
// data_policy.prompt_retention_days. Create defaults the top-level fields.
func privateProviderClearedFields(data *PrivateProviderResourceModel, isUpdate bool) []string {
	var fields []string
	if isUpdate && data.PrivacyPolicyURL.IsNull() {
		fields = append(fields, "privacy_policy_url")
	}
	if isUpdate && data.Headquarters.IsNull() {
		fields = append(fields, "headquarters")
	}
	if data.DataPolicy != nil && data.DataPolicy.PromptRetentionDays.IsNull() {
		fields = append(fields, "data_policy.prompt_retention_days")
	}
	return fields
}
