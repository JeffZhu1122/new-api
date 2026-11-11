package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/fingerprint_setting"
)

// HTTPTransportPolicy is the runtime-normalized outbound HTTP transport policy
// for a channel. Unknown or out-of-range stored values are clamped safely.
type HTTPTransportPolicy struct {
	Protocol              string // dto.HTTPProtocolAuto, dto.HTTPProtocolHTTP1 or dto.HTTPProtocolHTTP3
	Shards                int    // 1..dto.MaxHTTP2ConnectionShards
	TLSInsecureSkipVerify bool
	// Fingerprint names the fingerprint profile the channel selected. Empty
	// means the stock Go transport.
	Fingerprint string
	// FingerprintRevision identifies the profile content, so a client built
	// from an edited profile is never reused.
	FingerprintRevision string
}

var httpTransportPolicyWarnings sync.Map

func defaultHTTPTransportPolicy() HTTPTransportPolicy {
	return HTTPTransportPolicy{
		Protocol: dto.HTTPProtocolAuto,
		Shards:   1,
	}
}

// NormalizeHTTPTransportPolicy converts channel settings into a safe runtime policy.
// Invalid stored values never panic; they clamp to defaults and warn once per bad value.
func NormalizeHTTPTransportPolicy(settings dto.ChannelSettings) HTTPTransportPolicy {
	policy := defaultHTTPTransportPolicy()
	policy.TLSInsecureSkipVerify = settings.TLSInsecureSkipVerify

	protocol := strings.ToLower(strings.TrimSpace(settings.HTTPProtocol))
	switch protocol {
	case "", dto.HTTPProtocolAuto:
		policy.Protocol = dto.HTTPProtocolAuto
	case dto.HTTPProtocolHTTP1, dto.HTTPProtocolHTTP3:
		policy.Protocol = protocol
	default:
		warnHTTPTransportPolicyOnce("http_protocol", settings.HTTPProtocol)
		policy.Protocol = dto.HTTPProtocolAuto
	}

	shards := settings.HTTP2ConnectionShards
	switch {
	case shards == 0:
		policy.Shards = 1
	case shards < 1:
		warnHTTPTransportPolicyOnce("http2_connection_shards", fmt.Sprintf("%d", shards))
		policy.Shards = 1
	case shards > dto.MaxHTTP2ConnectionShards:
		warnHTTPTransportPolicyOnce("http2_connection_shards", fmt.Sprintf("%d", shards))
		policy.Shards = dto.MaxHTTP2ConnectionShards
	default:
		policy.Shards = shards
	}

	if policy.Protocol != dto.HTTPProtocolAuto {
		if settings.HTTP2ConnectionShards > 1 {
			warnHTTPTransportPolicyOnce(
				"http_protocol+http2_connection_shards",
				fmt.Sprintf("%s+%d", policy.Protocol, settings.HTTP2ConnectionShards),
			)
		}
		policy.Shards = 1
	}
	if policy.Shards < 1 {
		policy.Shards = 1
	}
	if settings.FingerprintProfile != "" && fingerprint_setting.IsEnabled() {
		policy.Fingerprint = settings.FingerprintProfile
		policy.FingerprintRevision = fingerprint_setting.Revision(settings.FingerprintProfile)
	}
	return policy
}

func warnHTTPTransportPolicyOnce(field, value string) {
	key := field + "=" + value
	if _, loaded := httpTransportPolicyWarnings.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	logger.LogWarn(
		context.Background(),
		common.LogText("invalid channel http transport setting clamped: %s=%q", field, value),
	)
}

func (p HTTPTransportPolicy) cacheKeyPart() string {
	key := fmt.Sprintf("%s|%d|%t", p.Protocol, p.Shards, p.TLSInsecureSkipVerify)
	if p.Fingerprint == "" {
		return key
	}
	return key + fingerprintCacheKeyMark + p.Fingerprint + "|" + p.FingerprintRevision
}

func (p HTTPTransportPolicy) String() string {
	return p.cacheKeyPart()
}
