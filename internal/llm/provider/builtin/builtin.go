// Package builtin registers the providers that ship with SafeAgent. Adding a provider means adding
// one file here and one line in All.
package builtin

import "github.com/yv1ing/SafeAgent/internal/llm/provider"

func All() []provider.ProviderSpec {
	return []provider.ProviderSpec{
		Anthropic(),
		OpenAI(),
		Google(),
		DeepSeek(),
		ZAI(),
		Qwen(),
		Custom(),
	}
}
