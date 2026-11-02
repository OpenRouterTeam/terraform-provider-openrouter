resource "openrouter_scim_group_mapping" "my_scimgroupmapping" {
  role          = "member"
  scim_group_id = "9f9d5869-724b-48da-a4a5-e5d8480fcbf4"
  workspace_id  = "813c2033-7245-4c1d-8434-1b393eb5d0af"

  # keep_members defaults to true on destroy: the mapping is deleted and the
  # workspace membership it granted is left intact. Set this to "false" to
  # also remove the members the mapping granted.
  keep_members_enum = "true"
}