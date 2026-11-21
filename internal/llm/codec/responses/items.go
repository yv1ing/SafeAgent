package responses

import (
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// EncodeInputItems renders stored input items in the flat Responses input shape, which is what the
// input_items listing endpoint returns.
func EncodeInputItems(items []model.Item) ([]any, []model.Loss, error) {
	var losses []model.Loss
	encoded := make([]any, 0, len(items))
	for index := range items {
		entry, nested, err := encodeItem(&items[index], index, codec.PartContext{Target: model.Responses, Role: items[index].Role})
		if err != nil {
			return nil, losses, err
		}
		losses = append(losses, nested...)
		if entry != nil {
			encoded = append(encoded, entry)
		}
	}
	return encoded, losses, nil
}
