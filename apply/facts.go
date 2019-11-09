package apply

import (
	"github.com/fastcat/wirelink/fact"
	"github.com/fastcat/wirelink/log"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// EnsureAllowedIPs updates the device config if needed to add all the
// AllowedIPs from the facts to the peer
func EnsureAllowedIPs(peer *wgtypes.Peer, facts []*fact.Fact, cfg *wgtypes.PeerConfig) *wgtypes.PeerConfig {
	curAIPs := make(map[string]bool)
	for _, aip := range peer.AllowedIPs {
		curAIPs[string(fact.IPNetValue{IPNet: aip}.Bytes())] = true
	}
	if cfg != nil {
		for _, aip := range cfg.AllowedIPs {
			curAIPs[string(fact.IPNetValue{IPNet: aip}.Bytes())] = true
		}
	}

	for _, f := range facts {
		switch f.Attribute {
		case fact.AttributeAllowedCidrV4:
			fallthrough
		case fact.AttributeAllowedCidrV6:
			if curAIPs[string(f.Value.Bytes())] {
				continue
			}
			if ipn, ok := f.Value.(*fact.IPNetValue); ok {
				if cfg == nil {
					cfg = &wgtypes.PeerConfig{PublicKey: peer.PublicKey}
				}
				cfg.AllowedIPs = append(cfg.AllowedIPs, ipn.IPNet)
			} else {
				log.Error("AIP Fact has wrong value type: %v => %T: %v", f.Attribute, f.Value, f.Value)
			}
		}
	}

	return cfg
}
