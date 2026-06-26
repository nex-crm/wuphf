interface OpenAICompatibleInputProps {
  endpointUrl: string;
  apiKey: string;
  onChangeUrl: (v: string) => void;
  onChangeKey: (v: string) => void;
}

/** Returns true when the string is a well-formed absolute URL. */
function isValidUrl(value: string): boolean {
  if (!value.trim()) return false;
  try {
    const u = new URL(value);
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}

/**
 * Two-field input for a custom OpenAI-protocol-compatible endpoint.
 *
 * The URL field validates format (must be an absolute http/https URL).
 * The API key field is masked. Both fields are optional independently:
 * the pair is only considered "filled" (usable for the canContinue gate)
 * when the URL is non-empty and valid. Empty = the section is not used.
 *
 * Wire shape: maps to ConfigUpdate.provider_endpoints["openai-compatible"]
 * via { base_url, ... }. The actual submit wires through PrePickScreen
 * which reads endpointUrl + apiKey from its parent state.
 */
export function OpenAICompatibleInput({
  endpointUrl,
  apiKey,
  onChangeUrl,
  onChangeKey,
}: OpenAICompatibleInputProps) {
  const urlTouched = endpointUrl.length > 0;
  const urlValid = !urlTouched || isValidUrl(endpointUrl);

  return (
    <div className="pre-pick-oai-compat" data-testid="pre-pick-oai-compat">
      <div className="pre-pick-oai-row">
        <label className="key-label" htmlFor="pre-pick-oai-url">
          Endpoint URL
        </label>
        <input
          id="pre-pick-oai-url"
          className={`input pre-pick-oai-input${urlTouched && !urlValid ? " pre-pick-input-error" : ""}`}
          type="url"
          placeholder="https://your-server/v1"
          value={endpointUrl}
          onChange={(e) => onChangeUrl(e.target.value)}
          autoComplete="off"
          data-testid="pre-pick-oai-url"
          aria-describedby={
            urlTouched && !urlValid ? "pre-pick-oai-url-err" : undefined
          }
        />
        {urlTouched && !urlValid ? (
          <span
            id="pre-pick-oai-url-err"
            className="pre-pick-oai-url-error"
            role="alert"
            data-testid="pre-pick-oai-url-error"
          >
            Enter a valid http:// or https:// URL
          </span>
        ) : null}
      </div>
      <div className="pre-pick-oai-row">
        <label className="key-label" htmlFor="pre-pick-oai-key">
          API key <span className="label-optional">(optional)</span>
        </label>
        <input
          id="pre-pick-oai-key"
          className="input pre-pick-oai-input"
          type="password"
          placeholder="sk-..."
          value={apiKey}
          onChange={(e) => onChangeKey(e.target.value)}
          autoComplete="off"
          data-testid="pre-pick-oai-key"
        />
      </div>
    </div>
  );
}

export { isValidUrl };
