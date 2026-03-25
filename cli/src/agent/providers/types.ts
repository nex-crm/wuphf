export type LLMProvider = 'gemini' | 'claude-code';

export interface ProviderConfig {
  provider: LLMProvider;
  geminiApiKey?: string;
}
