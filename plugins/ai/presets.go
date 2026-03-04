package ai

// ProviderPreset holds a pre-configured AI provider template.
type ProviderPreset struct {
	Name            string   `json:"name"`
	BaseURL         string   `json:"base_url"`
	APIFormat       string   `json:"api_format"`
	Models          []string `json:"models"`
	EmbeddingModels []string `json:"embedding_models"` // available embedding models (empty = not supported)
}

// ProviderPresets contains presets for popular AI providers.
var ProviderPresets = map[string]ProviderPreset{
	"openai": {
		Name:            "OpenAI",
		BaseURL:         "https://api.openai.com",
		APIFormat:       "openai-chat",
		Models:          []string{"gpt-4o", "gpt-4o-mini", "o3-mini"},
		EmbeddingModels: []string{"text-embedding-3-small", "text-embedding-3-large", "text-embedding-ada-002"},
	},
	"anthropic": {
		Name:            "Claude",
		BaseURL:         "https://api.anthropic.com",
		APIFormat:       "anthropic-messages",
		Models:          []string{"claude-sonnet-4-20250514", "claude-haiku-4-20250414"},
		EmbeddingModels: nil, // Anthropic does not support embeddings
	},
	"google": {
		Name:            "Gemini",
		BaseURL:         "https://generativelanguage.googleapis.com",
		APIFormat:       "google-generativeai",
		Models:          []string{"gemini-2.0-flash", "gemini-2.5-pro"},
		EmbeddingModels: nil, // Google uses a different embedding API format
	},
	"deepseek": {
		Name:            "DeepSeek",
		BaseURL:         "https://api.deepseek.com",
		APIFormat:       "openai-chat",
		Models:          []string{"deepseek-chat", "deepseek-reasoner"},
		EmbeddingModels: nil, // DeepSeek does not offer embedding models
	},
	"moonshot": {
		Name:            "Kimi",
		BaseURL:         "https://api.moonshot.cn",
		APIFormat:       "openai-chat",
		Models:          []string{"moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"},
		EmbeddingModels: nil, // Moonshot does not offer embedding models
	},
	"zhipu": {
		Name:            "智谱",
		BaseURL:         "https://open.bigmodel.cn/api/paas",
		APIFormat:       "openai-chat",
		Models:          []string{"glm-4-flash", "glm-4"},
		EmbeddingModels: []string{"embedding-3", "embedding-2"},
	},
}
