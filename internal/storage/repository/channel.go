package repository

import (
	"context"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"time"
)

func (store *Store) CreateChannel(ctx context.Context, channel *entity.Channel) (*entity.Channel, error) {
	channel.ID = secure.Random(12)
	channel.Version = 1
	channel.CreatedAt = time.Now().UTC()
	if _, err := store.DB.NewInsert().Model(channel).Exec(ctx); err != nil {
		return nil, err
	}
	return channel, nil
}

func (store *Store) UpdateChannel(ctx context.Context, id string, channel entity.Channel, version int64) (*entity.Channel, error) {
	result, err := store.DB.NewUpdate().Model((*entity.Channel)(nil)).
		Set("name = ?, format = ?, vendor = ?, endpoint = ?, count_tokens_endpoint = ?, headers = ?, models = ?, currency = ?, timeout_seconds = ?, first_byte_timeout_seconds = ?, allow_private_network = ?, enabled = ?, version = version + 1",
			channel.Name, channel.Format, channel.Vendor, channel.Endpoint, channel.CountTokensEndpoint, channel.Headers, channel.Models, channel.Currency, channel.TimeoutSeconds, channel.FirstByteTimeoutSeconds, channel.AllowPrivateNetwork, channel.Enabled).
		Where("id = ? AND version = ?", id, version).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrVersionConflict
	}
	return store.Channel(ctx, id)
}

func (store *Store) ChannelKeys(ctx context.Context, channelID string) ([]entity.ChannelKey, error) {
	keys := []entity.ChannelKey{}
	if err := store.DB.NewSelect().Model(&keys).Where("channel_id = ?", channelID).Order("id").Scan(ctx); err != nil {
		return nil, err
	}
	return keys, nil
}

func (store *Store) CreateChannelKey(ctx context.Context, channelID, label, secret, visibleSuffix string) (*entity.ChannelKey, error) {
	key := &entity.ChannelKey{ID: secure.Random(12), ChannelID: channelID, Label: label, Secret: secret, Suffix: visibleSuffix, Enabled: true}
	if _, err := store.DB.NewInsert().Model(key).Exec(ctx); err != nil {
		return nil, err
	}
	return key, nil
}

func (store *Store) DeleteChannelKey(ctx context.Context, channelID, id string) error {
	result, err := store.DB.NewDelete().Model((*entity.ChannelKey)(nil)).Where("id = ? AND channel_id = ?", id, channelID).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func prefix(secret string) string {
	if len(secret) <= 8 {
		return secret
	}
	return secret[:8]
}

func (store *Store) Channels(ctx context.Context) ([]entity.Channel, error) {
	channels := []entity.Channel{}
	if err := store.DB.NewSelect().Model(&channels).Order("name").Scan(ctx); err != nil {
		return nil, err
	}
	return channels, nil
}

func (store *Store) Channel(ctx context.Context, id string) (*entity.Channel, error) {
	channel := new(entity.Channel)
	if err := store.DB.NewSelect().Model(channel).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return channel, nil
}

// EnabledChannelKeys returns every usable upstream credential, grouped by channel by the caller.
func (store *Store) EnabledChannelKeys(ctx context.Context) ([]entity.ChannelKey, error) {
	keys := []entity.ChannelKey{}
	if err := store.DB.NewSelect().Model(&keys).Where("enabled").OrderExpr("channel_id, id").Scan(ctx); err != nil {
		return nil, err
	}
	return keys, nil
}
