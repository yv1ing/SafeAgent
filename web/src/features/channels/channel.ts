import type { ConsoleChannelView, ConsoleUpdateChannelInput } from '../../api/generated/models';

// The API masks secret header values on read; echoing the mask back on update makes the service keep
// the stored value, so a plain round trip never exposes or clears a credential.
export function channelInput(channel: ConsoleChannelView): ConsoleUpdateChannelInput {
  return {
    name: channel.name,
    provider_id: channel.provider_id,
    base_url: channel.base_url ?? '',
    headers: channel.headers ?? {},
    forward_headers: channel.forward_headers ?? [],
    default_max_output_tokens: channel.default_max_output_tokens ?? 0,
    timeout_seconds: channel.timeout_seconds ?? 0,
    first_byte_timeout_seconds: channel.first_byte_timeout_seconds ?? 0,
    allow_private_network: channel.allow_private_network ?? false,
    enabled: channel.enabled ?? true,
    version: channel.version,
  };
}
