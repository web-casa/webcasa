package ai

// ProviderPreset holds a pre-configured AI provider template.
type ProviderPreset struct {
	Name      string   `json:"name"`
	BaseURL   string   `json:"base_url"`
	APIFormat string   `json:"api_format"`
	Models    []string `json:"models"`
}

// ProviderPresets contains presets for popular AI providers.
var ProviderPresets = map[string]ProviderPreset{
	"openai": {
		Name:      "OpenAI",
		BaseURL:   "https://api.openai.com",
		APIFormat: "openai-chat",
		Models:    []string{"gpt-4o", "gpt-4o-mini", "o3-mini"},
	},
	"anthropic": {
		Name:      "Claude",
		BaseURL:   "https://api.anthropic.com",
		APIFormat: "anthropic-messages",
		Models:    []string{"claude-sonnet-4-20250514", "claude-haiku-4-20250414"},
	},
	"google": {
		Name:      "Gemini",
		BaseURL:   "https://generativelanguage.googleapis.com",
		APIFormat: "google-generativeai",
		Models:    []string{"gemini-2.0-flash", "gemini-2.5-pro"},
	},
	"deepseek": {
		Name:      "DeepSeek",
		BaseURL:   "https://api.deepseek.com",
		APIFormat: "openai-chat",
		Models:    []string{"deepseek-chat", "deepseek-reasoner"},
	},
	"moonshot": {
		Name:      "Kimi",
		BaseURL:   "https://api.moonshot.cn",
		APIFormat: "openai-chat",
		Models:    []string{"moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"},
	},
	"zhipu": {
		Name:      "智谱",
		BaseURL:   "https://open.bigmodel.cn/api/paas",
		APIFormat: "openai-chat",
		Models:    []string{"glm-4-flash", "glm-4"},
	},
}
