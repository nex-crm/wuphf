package action

// composio_apikey.go — connect path for toolkits that authenticate with a
// user-supplied API key / token rather than Composio-managed OAuth.
//
// Background: StartIntegrationConnection used to ALWAYS create an auth config
// of type "use_composio_managed_auth". That only works for toolkits where
// Composio hosts its own OAuth app (Gmail, Slack, …). For an API-key toolkit
// like Instantly, Composio has no managed app, so POST /auth_configs returns a
// bare 400 ("400 POST /auth_configs") with no guidance — the exact customer
// bug this module fixes.
//
// The flow is now: detect the toolkit's auth mode (GET /toolkits/{slug}); if
// Composio can manage it, keep the OAuth path; otherwise surface the required
// credential fields so the UI can collect them, then create a custom-auth
// config + connected account with the user's key.
//
// NOTE: the Composio v3 request/response shapes below follow the documented
// API but have not been exercised against a live API-key toolkit in this
// change. The shapes are isolated here and unit-tested for what WE send so a
// field-name tweak after live verification is a one-line change.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// composioManagedAuthSchemes are the schemes Composio can host an app for, so
// the existing zero-touch OAuth path applies. Everything else needs the user
// to bring their own credential.
var composioManagedAuthSchemes = map[string]bool{
	"OAUTH2":  true,
	"OAUTH1":  true,
	"OAUTH1A": true,
}

// toolkitAuthInfo is the distilled auth shape for one toolkit.
type toolkitAuthInfo struct {
	// Managed is true when Composio hosts an OAuth app for this toolkit, so the
	// existing use_composio_managed_auth + hosted-link flow works.
	Managed bool
	// Mode is the primary non-managed auth scheme (e.g. "API_KEY", "BEARER_TOKEN",
	// "BASIC"). Empty when the toolkit is managed or exposes no scheme.
	Mode string
	// Fields are the credentials the user must supply for Mode.
	Fields []IntegrationConnectField
}

// composioToolkitDetail mirrors the slice of GET /toolkits/{slug} we read.
type composioToolkitDetail struct {
	Slug                       string   `json:"slug"`
	Name                       string   `json:"name"`
	ComposioManagedAuthSchemes []string `json:"composio_managed_auth_schemes"`
	AuthConfigDetails          []struct {
		Mode   string `json:"mode"`
		Fields struct {
			ConnectedAccountInitiation composioFieldSet `json:"connected_account_initiation"`
		} `json:"fields"`
	} `json:"auth_config_details"`
}

type composioFieldSet struct {
	Required []composioAuthField `json:"required"`
	Optional []composioAuthField `json:"optional"`
}

type composioAuthField struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
}

// toolkitAuthInfo fetches a toolkit's auth detail and distills it. On any error
// it returns Managed=true so callers fall back to the existing OAuth attempt
// rather than blocking a toolkit we simply could not introspect.
func (c *ComposioREST) toolkitAuthInfo(ctx context.Context, platform string) toolkitAuthInfo {
	raw, err := c.get(ctx, "/toolkits/"+url.PathEscape(platform), nil)
	if err != nil {
		return toolkitAuthInfo{Managed: true}
	}
	var detail composioToolkitDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return toolkitAuthInfo{Managed: true}
	}

	for _, scheme := range detail.ComposioManagedAuthSchemes {
		if composioManagedAuthSchemes[strings.ToUpper(strings.TrimSpace(scheme))] {
			return toolkitAuthInfo{Managed: true}
		}
	}

	// No managed OAuth scheme — scan the modes for the first non-OAuth one and
	// surface its credential fields. OAuth-ish (or empty) modes are skipped:
	// the user has no client credentials to paste for an OAuth app Composio
	// doesn't host, so those fall through to the managed fallback below.
	for _, ad := range detail.AuthConfigDetails {
		mode := strings.ToUpper(strings.TrimSpace(ad.Mode))
		if mode == "" || composioManagedAuthSchemes[mode] {
			continue
		}
		return toolkitAuthInfo{
			Mode:   mode,
			Fields: connectFieldsFrom(ad.Fields.ConnectedAccountInitiation, mode),
		}
	}

	// No actionable non-OAuth mode — fall back to managed so we don't regress.
	return toolkitAuthInfo{Managed: true}
}

// connectFieldsFrom maps Composio's field metadata to our wire shape. When the
// toolkit advertises no initiation fields we synthesise a single API-key field
// so the user still gets a usable form for the common API_KEY case.
func connectFieldsFrom(set composioFieldSet, mode string) []IntegrationConnectField {
	all := append(append([]composioAuthField{}, set.Required...), set.Optional...)
	out := make([]IntegrationConnectField, 0, len(all))
	for _, f := range all {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		out = append(out, IntegrationConnectField{
			Name:        name,
			Label:       firstNonEmpty(f.DisplayName, humanizeFieldName(name)),
			Description: strings.TrimSpace(f.Description),
			Secret:      isSecretField(name, f.Type),
			Required:    f.Required || containsField(set.Required, name),
		})
	}
	if len(out) == 0 {
		out = append(out, defaultCredentialField(mode))
	}
	return out
}

func containsField(fields []composioAuthField, name string) bool {
	for _, f := range fields {
		if strings.EqualFold(strings.TrimSpace(f.Name), name) {
			return true
		}
	}
	return false
}

// defaultCredentialField is the fallback input when a toolkit exposes no
// initiation field metadata — almost always a single API key / token.
func defaultCredentialField(mode string) IntegrationConnectField {
	switch mode {
	case "BEARER_TOKEN":
		return IntegrationConnectField{Name: "token", Label: "Access token", Secret: true, Required: true}
	case "BASIC":
		return IntegrationConnectField{Name: "password", Label: "Password", Secret: true, Required: true}
	default:
		return IntegrationConnectField{Name: "generic_api_key", Label: "API key", Secret: true, Required: true}
	}
}

func isSecretField(name, fieldType string) bool {
	n := strings.ToLower(name)
	if strings.Contains(strings.ToLower(fieldType), "password") {
		return true
	}
	for _, hint := range []string{"key", "token", "secret", "password"} {
		if strings.Contains(n, hint) {
			return true
		}
	}
	return false
}

func humanizeFieldName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	if len(parts) == 0 {
		return name
	}
	return strings.Join(parts, " ")
}

// CompleteAPIKeyConnection creates a custom-auth config and a connected account
// for an API-key/token toolkit using the user-supplied credentials. Returns a
// connected (or pending) result the UI can poll like the OAuth path.
func (c *ComposioREST) CompleteAPIKeyConnection(ctx context.Context, platform string, fields map[string]string) (IntegrationConnectResult, error) {
	platform = normalizeComposioPlatform(platform)
	if platform == "" {
		return IntegrationConnectResult{}, fmt.Errorf("platform is required")
	}
	if len(fields) == 0 {
		return IntegrationConnectResult{}, fmt.Errorf("no credentials supplied")
	}
	info := c.toolkitAuthInfo(ctx, platform)
	mode := info.Mode
	if mode == "" {
		mode = "API_KEY"
	}

	// Drop blank values and confirm every required credential is present BEFORE
	// touching Composio. A missing/blank required field would otherwise create
	// an auth config that then fails at /connected_accounts, leaving an orphan
	// auth config behind.
	cleaned := make(map[string]string, len(fields))
	for k, v := range fields {
		if tv := strings.TrimSpace(v); tv != "" {
			cleaned[k] = tv
		}
	}
	for _, f := range info.Fields {
		if f.Required {
			if _, ok := cleaned[f.Name]; !ok {
				return IntegrationConnectResult{}, fmt.Errorf("missing required credential: %s", f.Name)
			}
		}
	}
	if len(cleaned) == 0 {
		return IntegrationConnectResult{}, fmt.Errorf("no credentials supplied")
	}

	authConfigID, err := c.createCustomAuthConfig(ctx, platform, mode)
	if err != nil {
		return IntegrationConnectResult{}, fmt.Errorf("create custom auth config: %w", err)
	}

	// Connected account carries the user's credentials in the scheme-specific
	// `val` map. Field names come from the toolkit's initiation metadata, so
	// whatever Composio asked for is exactly what we send back.
	body := map[string]any{
		"auth_config": map[string]any{"id": authConfigID},
		"connection": map[string]any{
			"user_id": strings.TrimSpace(c.UserID),
			"state": map[string]any{
				"authScheme": mode,
				"val":        stringMapToAny(cleaned),
			},
		},
	}
	raw, err := c.post(ctx, "/connected_accounts", body)
	if err != nil {
		return IntegrationConnectResult{}, fmt.Errorf("create connected account: %w", err)
	}
	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return IntegrationConnectResult{}, fmt.Errorf("parse connected account: %w", err)
	}
	status := connectionState(result.Status)

	// Verify the credential actually works before declaring the toolkit
	// connected. Composio accepts API-key credentials at creation time WITHOUT
	// testing them — every API-key account is reported ACTIVE on create — so a
	// bogus key would otherwise surface as a false "Connected".
	switch c.validateAPIKeyConnection(ctx, result.ID) {
	case verdictRejected:
		// Composio checked the key and it failed. Remove the orphan account so a
		// retry starts clean, then surface a clear error the UI shows instead of
		// a fake success.
		c.deleteConnectedAccountBestEffort(ctx, result.ID)
		return IntegrationConnectResult{}, fmt.Errorf("%s rejected the credentials — double-check the key and try again", DisplayPlatformName(platform))
	case verdictValid:
		status = "connected"
	case verdictUnverified:
		// Composio's experimental validation path is unavailable for this
		// toolkit/plan, so we could not test the key. Fall back to the
		// create-time status rather than blocking a connection we couldn't
		// check. A blank/unknown create status for a non-OAuth account is
		// treated as active, matching prior behavior.
		if status == "available" {
			status = "connected"
		}
	}
	return IntegrationConnectResult{
		Provider:      c.Name(),
		Platform:      platform,
		Status:        status,
		ConnectID:     strings.TrimSpace(result.ID),
		ConnectionKey: strings.TrimSpace(result.ID),
		AuthMode:      strings.ToLower(mode),
	}, nil
}

// createCustomAuthConfig creates a use_custom_auth config for a non-OAuth
// toolkit. The user's credential lands on the connected account, not here, so
// this just declares "this toolkit will be connected with a custom scheme".
func (c *ComposioREST) createCustomAuthConfig(ctx context.Context, platform, mode string) (string, error) {
	body := map[string]any{
		"toolkit": map[string]any{"slug": platform},
		"auth_config": map[string]any{
			"type":        "use_custom_auth",
			"authScheme":  mode,
			"name":        "WUPHF " + DisplayPlatformName(platform),
			"credentials": map[string]any{},
		},
	}
	raw, err := c.post(ctx, "/auth_configs", body)
	if err != nil {
		return "", err
	}
	var result struct {
		ID         string             `json:"id"`
		AuthConfig composioAuthConfig `json:"auth_config"`
		Item       composioAuthConfig `json:"item"`
		Data       composioAuthConfig `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parse custom auth config: %w", err)
	}
	if id := strings.TrimSpace(firstNonEmpty(result.ID, result.AuthConfig.ID, result.Item.ID, result.Data.ID)); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("custom auth config response did not include id")
}

// apiKeyVerdict is the outcome of asking Composio to validate a freshly created
// API-key connected account.
type apiKeyVerdict int

const (
	// verdictUnverified: Composio could not (or would not) tell us whether the
	// credential works. The validate path is experimental and not available for
	// every toolkit/plan, so the caller keeps the create-time status rather than
	// blocking a connection it simply could not test.
	verdictUnverified apiKeyVerdict = iota
	// verdictValid: Composio confirmed the credential is live.
	verdictValid
	// verdictRejected: Composio actively rejected the credential.
	verdictRejected
)

// validateAPIKeyConnection asks Composio to verify the credential on a freshly
// created connected account. Composio marks API-key accounts ACTIVE at creation
// WITHOUT checking the key, so without this a bogus key reads as "Connected".
// The refresh endpoint's experimental `validate_credentials` flag re-checks the
// stored credential for API-key auth schemes specifically.
func (c *ComposioREST) validateAPIKeyConnection(ctx context.Context, connectedAccountID string) apiKeyVerdict {
	id := strings.TrimSpace(connectedAccountID)
	if id == "" {
		return verdictUnverified
	}
	raw, err := c.post(ctx, "/connected_accounts/"+url.PathEscape(id)+"/refresh", map[string]any{
		"validate_credentials": true,
	})
	if err != nil {
		// A definitive auth rejection (401/403) or unprocessable credential (422)
		// means the key was checked and failed. Any other failure — 404 (toolkit
		// has no validation path), 5xx, or a network error — means we could not
		// test the key, so we treat it as unverified rather than blocking a
		// connection that may be perfectly valid.
		var apiErr *ComposioAPIError
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity:
				return verdictRejected
			}
		}
		return verdictUnverified
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return verdictUnverified
	}
	switch connectionState(result.Status) {
	case "connected":
		return verdictValid
	case "available":
		// Empty/unknown status — nothing actionable to assert either way.
		return verdictUnverified
	default:
		// failed, pending, disconnected, … — the credential did not come up live.
		return verdictRejected
	}
}

// deleteConnectedAccountBestEffort removes a connected account, ignoring errors.
// Used to clean up an account whose credential Composio rejected so a retry is
// not blocked by a dangling, non-working connection.
func (c *ComposioREST) deleteConnectedAccountBestEffort(ctx context.Context, connectedAccountID string) {
	id := strings.TrimSpace(connectedAccountID)
	if id == "" {
		return
	}
	_, _ = c.delete(ctx, "/connected_accounts/"+url.PathEscape(id))
}

func stringMapToAny(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
