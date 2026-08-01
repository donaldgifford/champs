# Example champs configuration.
#
# Copy this file to champs.hcl and edit. champs reads ./champs.hcl by
# default; point at another path with --config.

# Settings for the per-org security-champions team. champs applies these
# only when it creates the team — an existing team's settings are never
# modified.
team {
  slug        = "security_champions"
  description = "Security champions with existing access to this org"

  # "closed" (visible to org members) or "secret". Defaults to "closed"
  # when omitted.
  privacy = "closed"
}

# The GitHub App champs authenticates as. The App needs the
# "Organization permissions -> Members: Read and write" permission and
# must be installed on every org listed below.
github {
  app_id = 12345

  # Path to the App's PEM private key. When the CHAMPS_GITHUB_PRIVATE_KEY
  # environment variable is set (to the PEM contents, not a path) it wins
  # over this — preferred in CI so the key never touches disk. At least
  # one of the two sources must be available.
  private_key_path = "/path/to/champs.private-key.pem"
}

# Managed organizations, one block per org, no duplicates. Orgs where the
# App is not installed are reported as no_installation skips — never a
# crash, and never an installation attempt.
org "org1" {}
org "org2" {}
org "org3" {}
