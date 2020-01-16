package config

import (
	"net"

	"github.com/fastcat/wirelink/trust"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Peers represents a set of peer configs, with handy access methods that avoid
// boiler plate for peers that are not configured
type Peers map[wgtypes.Key]*Peer

// Name returns the name of the peer, if configured, or else its key string
func (p Peers) Name(peer wgtypes.Key) string {
	if config, ok := p[peer]; ok {
		return config.Name
	}
	return peer.String()
}

// Trust returns the configured trust level (if present and valid) or else the provided default
func (p Peers) Trust(peer wgtypes.Key, def trust.Level) trust.Level {
	if config, ok := p[peer]; ok && config.Trust != nil {
		return *config.Trust
	}
	// else fall through
	return def
}

// IsFactExchanger returns true if the peer is configured as a FactExchanger
func (p Peers) IsFactExchanger(peer wgtypes.Key) bool {
	if config, ok := p[peer]; ok && config.FactExchanger {
		return true
	}
	return false
}

// IsBasic returns true if the peer is explicitly configured as a basic peer,
// or false otherwise
func (p Peers) IsBasic(peer wgtypes.Key) bool {
	if config, ok := p[peer]; ok && config.Basic {
		return true
	}
	return false
}

// AllowedIPs returns the array of AllowedIPs explicitly configured for the peer, if any
func (p Peers) AllowedIPs(peer wgtypes.Key) []net.IPNet {
	if config, ok := p[peer]; ok {
		return config.AllowedIPs
	}
	return nil
}

// Endpoints returns the array of Endpoints explicitly configured for the peer, if any
func (p Peers) Endpoints(peer wgtypes.Key) []PeerEndpoint {
	if config, ok := p[peer]; ok {
		return config.Endpoints
	}
	return nil
}
