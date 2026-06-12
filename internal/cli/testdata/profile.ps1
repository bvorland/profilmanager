# Minimal fixture for import-mj tests. Mirrors the shape of the real
# $script:ProfilesList block in sample/profile.ps1, just smaller.
$script:ProfilesList = @(
  @{name="Acme.Dev";   label="🟢 Acme Dev";   color="Green"}
  @{name="Acme.Prod";  label="🔴 Acme Prod";  color="Red"}
  @{name="Sandbox-1";  label="🟡 Sandbox";    color="Yellow"}
)
