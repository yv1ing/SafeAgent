package console

import (
	"context"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (service *ChannelService) Keys(ctx context.Context, actor Actor, channelID string) ([]entity.ChannelKey, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if _, err := service.store.Channel(ctx, channelID); err != nil {
		return nil, Translate(err)
	}
	return service.store.ChannelKeys(ctx, channelID)
}

func (service *ChannelService) AddKey(ctx context.Context, actor Actor, channelID, label, secret string) (*entity.ChannelKey, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	label, err := OptionalText(label, MaximumKeyLabelLength, "invalid_label")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(secret) == "" || len(secret) > MaximumSecretLength || strings.ContainsAny(secret, "\r\n\x00") {
		return nil, Invalid("invalid_secret")
	}
	if _, err := service.store.Channel(ctx, channelID); err != nil {
		return nil, Translate(err)
	}
	plain := strings.TrimSpace(secret)
	sealed, err := secure.Seal(service.store.Secret, []byte(plain))
	if err != nil {
		return nil, err
	}
	suffix := "****"
	if len(plain) > 4 {
		suffix = plain[len(plain)-4:]
	}
	key, err := service.store.CreateChannelKey(ctx, channelID, label, sealed, suffix)
	return key, Translate(err)
}

func (service *ChannelService) DeleteKey(ctx context.Context, actor Actor, channelID, keyID string) error {
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	return Translate(service.store.DeleteChannelKey(ctx, channelID, keyID))
}
