package codec

import (
	"fmt"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func ParseResponseFormat(value any) (*model.ResponseFormat, error) {
	if value == nil {
		return nil, nil
	}
	object := model.Object(value)
	if object == nil {
		return nil, fmt.Errorf("output format must be an object")
	}
	result := &model.ResponseFormat{Kind: model.String(object["type"])}
	switch result.Kind {
	case "text", "json_object":
		return result, nil
	case "json_schema":
		if nested := model.Object(object["json_schema"]); nested != nil {
			object = nested
		}
		result.Name = model.String(object["name"])
		result.Description = model.String(object["description"])
		schema := model.Object(object["schema"])
		if schema == nil {
			return nil, fmt.Errorf("structured output requires a JSON schema object")
		}
		result.Schema = model.Marshal(schema)
		if strict, ok := object["strict"].(bool); ok {
			result.Strict = &strict
		}
		return result, nil
	}
	return nil, fmt.Errorf("unsupported output format %q", result.Kind)
}

func encodeResponseFormat(responseFormat *model.ResponseFormat, target model.Format, root map[string]any) []model.Loss {
	var losses []model.Loss
	schema := model.DecodeJSONOrNil(responseFormat.Schema)
	name := responseFormat.Name
	if name == "" {
		name = "response"
	}
	switch target {
	case model.Chat:
		entry := map[string]any{"type": responseFormat.Kind}
		if responseFormat.Kind == "json_schema" {
			definition := map[string]any{"name": name, "schema": schema}
			if responseFormat.Description != "" {
				definition["description"] = responseFormat.Description
			}
			if responseFormat.Strict != nil {
				definition["strict"] = *responseFormat.Strict
			}
			entry["json_schema"] = definition
		}
		root["response_format"] = entry
	case model.Responses:
		entry := map[string]any{"type": responseFormat.Kind}
		if responseFormat.Kind == "json_schema" {
			entry["name"], entry["schema"] = name, schema
			if responseFormat.Description != "" {
				entry["description"] = responseFormat.Description
			}
			if responseFormat.Strict != nil {
				entry["strict"] = *responseFormat.Strict
			}
		}
		text := model.Object(root["text"])
		if text == nil {
			text = map[string]any{}
			root["text"] = text
		}
		text["format"] = entry
	case model.Messages:
		switch responseFormat.Kind {
		case "json_schema":
			config := model.Object(root["output_config"])
			if config == nil {
				config = map[string]any{}
				root["output_config"] = config
			}
			config["format"] = map[string]any{"type": "json_schema", "schema": schema}
			if responseFormat.Name != "" || responseFormat.Description != "" || responseFormat.Strict != nil {
				losses = append(losses, dropped("response_format", "Messages structured output has no schema name, description or strict flag", target))
			}
		case "json_object":
			losses = append(losses, dropped("response_format", "Messages has no schema-less JSON mode", target))
		}
	case model.Gemini:
		config := generationConfig(root)
		switch responseFormat.Kind {
		case "text":
			config["responseMimeType"] = "text/plain"
		case "json_object":
			config["responseMimeType"] = "application/json"
		case "json_schema":
			config["responseMimeType"] = "application/json"
			config["responseJsonSchema"] = schema
			if responseFormat.Name != "" || responseFormat.Description != "" || responseFormat.Strict != nil {
				losses = append(losses, dropped("response_format", "Gemini has no schema name, description or strict flag", target))
			}
		}
	}
	return losses
}
