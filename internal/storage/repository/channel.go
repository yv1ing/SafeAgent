package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// ChannelRecord is one channel with the facts the console shows next to it: its provider label, how
// many routes reference it, and the upstream capability rejections learned at runtime.
type ChannelRecord struct {
	entity.Channel
	ProviderLabel       string
	RouteCount          int
	CapabilityOverrides []entity.CapabilityOverride
}

type channelRouteCount struct {
	ChannelID string `bun:"channel_id"`
	Count     int    `bun:"count"`
}

type providerLabel struct {
	ID    string `bun:"id"`
	Label string `bun:"label"`
}

func (store *Store) Channels(ctx context.Context, includeDeleted bool) ([]ChannelRecord, error) {
	channels := []entity.Channel{}
	query := store.DB.NewSelect().Model(&channels).Order("name", "id")
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	return store.attachChannelRecords(ctx, channels)
}

func (store *Store) Channel(ctx context.Context, id string) (*ChannelRecord, error) {
	channel := new(entity.Channel)
	if err := store.DB.NewSelect().Model(channel).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	records, err := store.attachChannelRecords(ctx, []entity.Channel{*channel})
	if err != nil {
		return nil, err
	}
	return &records[0], nil
}

func (store *Store) attachChannelRecords(ctx context.Context, channels []entity.Channel) ([]ChannelRecord, error) {
	labels := map[string]string{}
	rows := []providerLabel{}
	if err := store.DB.NewRaw(`SELECT id, label FROM providers`).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		labels[row.ID] = row.Label
	}
	counts := map[string]int{}
	references := []channelRouteCount{}
	if err := store.DB.NewRaw(`SELECT rt.channel_id, count(DISTINCT rt.route_id) AS count
		FROM route_targets rt JOIN model_routes r ON r.id = rt.route_id AND r.deleted_at IS NULL
		GROUP BY 1`).Scan(ctx, &references); err != nil {
		return nil, err
	}
	for _, reference := range references {
		counts[reference.ChannelID] = reference.Count
	}
	ids := make([]string, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.ID)
	}
	overrides := []entity.CapabilityOverride{}
	if len(ids) > 0 {
		if err := store.DB.NewSelect().Model(&overrides).
			Where("channel_id IN (?)", bun.In(ids)).
			Order("channel_id", "model_id", "format", "field").Scan(ctx); err != nil {
			return nil, err
		}
	}
	byChannel := map[string][]entity.CapabilityOverride{}
	for _, override := range overrides {
		byChannel[override.ChannelID] = append(byChannel[override.ChannelID], override)
	}
	records := make([]ChannelRecord, 0, len(channels))
	for _, channel := range channels {
		records = append(records, ChannelRecord{
			Channel: channel, ProviderLabel: labels[channel.ProviderID],
			RouteCount: counts[channel.ID], CapabilityOverrides: byChannel[channel.ID],
		})
	}
	return records, nil
}

func (store *Store) CreateChannel(ctx context.Context, channel *entity.Channel) (*ChannelRecord, error) {
	channel.ID = secure.Random(12)
	channel.Version = 1
	channel.CreatedAt = time.Now().UTC()
	channel.UpdatedAt = channel.CreatedAt
	channel.DeletedAt = nil
	if channel.Headers == nil {
		channel.Headers = map[string]string{}
	}
	if channel.ForwardHeaders == nil {
		channel.ForwardHeaders = []string{}
	}
	if _, err := store.DB.NewInsert().Model(channel).Exec(ctx); err != nil {
		return nil, err
	}
	return store.Channel(ctx, channel.ID)
}

func (store *Store) UpdateChannel(ctx context.Context, id string, channel entity.Channel, version int64) (*ChannelRecord, error) {
	if channel.Headers == nil {
		channel.Headers = map[string]string{}
	}
	if channel.ForwardHeaders == nil {
		channel.ForwardHeaders = []string{}
	}
	result, err := store.DB.NewUpdate().Model((*entity.Channel)(nil)).
		Set("name = ?, provider_id = ?, base_url = ?, headers = ?, forward_headers = ?, default_max_output_tokens = ?, timeout_seconds = ?, first_byte_timeout_seconds = ?, allow_private_network = ?, enabled = ?, updated_at = ?, version = version + 1",
			channel.Name, channel.ProviderID, channel.BaseURL, channel.Headers, channel.ForwardHeaders,
			channel.DefaultMaxOutputTokens, channel.TimeoutSeconds, channel.FirstByteTimeoutSeconds,
			channel.AllowPrivateNetwork, channel.Enabled, time.Now().UTC()).
		Where("id = ? AND deleted_at IS NULL AND version = ?", id, version).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrVersionConflict
	}
	return store.Channel(ctx, id)
}

// DeleteChannel soft-deletes one channel. With detachTargets the route targets that point at it are
// removed; otherwise they stay and are disabled, so a route degrades instead of disappearing. Either
// way the owning routes advance their version, so a stale route edit cannot write the old targets
// back.
func (store *Store) DeleteChannel(ctx context.Context, id string, detachTargets bool) error {
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		now := time.Now().UTC()
		result, err := transaction.NewUpdate().Model((*entity.Channel)(nil)).
			Set("deleted_at = ?, enabled = false, updated_at = ?, version = version + 1", now, now).
			Where("id = ? AND deleted_at IS NULL", id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrNotFound
		}
		if err := bumpRouteVersionsOfTargets(ctx, transaction, now, "channel_id = ?", id); err != nil {
			return err
		}
		if detachTargets {
			_, err := transaction.NewDelete().Model((*entity.RouteTarget)(nil)).Where("channel_id = ?", id).Exec(ctx)
			return err
		}
		return disableTargetsOfChannel(ctx, transaction, id, "channel deleted")
	})
}

// RestoreChannel clears the soft deletion and re-enables the targets it disabled by itself. A
// target an operator disabled for another reason stays disabled, and the owning routes advance
// their version so their target set is re-read.
func (store *Store) RestoreChannel(ctx context.Context, id string) (*ChannelRecord, error) {
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		now := time.Now().UTC()
		result, err := transaction.NewUpdate().Model((*entity.Channel)(nil)).
			Set("deleted_at = NULL, enabled = true, updated_at = ?, version = version + 1", now).
			Where("id = ? AND deleted_at IS NOT NULL", id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrNotFound
		}
		if err := bumpRouteVersionsOfTargets(ctx, transaction, now, "channel_id = ? AND enabled = false AND disabled_reason = ?", id, "channel deleted"); err != nil {
			return err
		}
		_, err = transaction.NewUpdate().Model((*entity.RouteTarget)(nil)).
			Set("enabled = true, disabled_reason = ''").
			Where("channel_id = ? AND disabled_reason = ?", id, "channel deleted").Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return store.Channel(ctx, id)
}

// CreateChannelBundle writes a provider (when it is new), the channel, the catalog models and the
// channel keys in one transaction. The console's connect flow uses it so a failed connection leaves
// no half-configured provider behind.
func (store *Store) CreateChannelBundle(ctx context.Context, provider *entity.Provider, channel *entity.Channel, models []*entity.Model, keys []entity.ChannelKey) error {
	now := time.Now().UTC()
	if provider != nil {
		provider.ID = secure.Random(12)
		provider.Source = entity.ProviderCustom
		provider.Version = 1
		provider.CreatedAt, provider.UpdatedAt = now, now
	}
	if channel.ProviderID == "" && provider != nil {
		channel.ProviderID = provider.ID
	}
	channel.ID = secure.Random(12)
	channel.Version = 1
	channel.CreatedAt, channel.UpdatedAt = now, now
	if channel.Headers == nil {
		channel.Headers = map[string]string{}
	}
	if channel.ForwardHeaders == nil {
		channel.ForwardHeaders = []string{}
	}
	for _, model := range models {
		model.ID = secure.Random(12)
		model.ProviderID = channel.ProviderID
		model.Version = 1
		model.CreatedAt, model.UpdatedAt = now, now
	}
	for index := range keys {
		keys[index].ID = secure.Random(12)
		keys[index].ChannelID = channel.ID
		keys[index].Enabled = true
	}
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if provider != nil {
			if _, err := transaction.NewInsert().Model(provider).Exec(ctx); err != nil {
				return err
			}
		}
		for _, model := range models {
			if _, err := transaction.NewInsert().Model(model).Exec(ctx); err != nil {
				if IsUniqueViolation(err) {
					return &ModelNameConflictError{UpstreamName: model.UpstreamName}
				}
				return err
			}
		}
		if _, err := transaction.NewInsert().Model(channel).Exec(ctx); err != nil {
			return err
		}
		if len(keys) == 0 {
			return nil
		}
		_, err := transaction.NewInsert().Model(&keys).Exec(ctx)
		return err
	})
}

// AddCapabilityOverride records one fact an upstream rejected; the write is an upsert because the
// same rejection is observed again on every retry.
func (store *Store) AddCapabilityOverride(ctx context.Context, override entity.CapabilityOverride) error {
	if len(override.Value) == 0 {
		override.Value = json.RawMessage(`{}`)
	}
	override.CreatedAt = time.Now().UTC()
	_, err := store.DB.NewInsert().Model(&override).
		On("CONFLICT (channel_id, model_id, format, field) DO UPDATE SET value = EXCLUDED.value, reason = EXCLUDED.reason").
		Exec(ctx)
	return err
}

func (store *Store) CapabilityOverrides(ctx context.Context, channelIDs []string) ([]entity.CapabilityOverride, error) {
	overrides := []entity.CapabilityOverride{}
	if len(channelIDs) == 0 {
		return overrides, nil
	}
	err := store.DB.NewSelect().Model(&overrides).
		Where("channel_id IN (?)", bun.In(channelIDs)).
		Order("channel_id", "model_id", "format", "field").Scan(ctx)
	return overrides, err
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

// EnabledChannelKeys returns every usable upstream credential; keys of a disabled or deleted
// channel are not offered to the runtime.
func (store *Store) EnabledChannelKeys(ctx context.Context) ([]entity.ChannelKey, error) {
	keys := []entity.ChannelKey{}
	err := store.DB.NewRaw(`SELECT ck.* FROM channel_keys ck
		JOIN channels ch ON ch.id = ck.channel_id AND ch.enabled AND ch.deleted_at IS NULL
		WHERE ck.enabled ORDER BY ck.channel_id, ck.id`).Scan(ctx, &keys)
	return keys, err
}

func disableTargetsOfChannel(ctx context.Context, db bun.IDB, channelID, reason string) error {
	_, err := db.NewUpdate().Model((*entity.RouteTarget)(nil)).
		Set("enabled = false, disabled_reason = ?", reason).
		Where("channel_id = ? AND enabled", channelID).Exec(ctx)
	return err
}
