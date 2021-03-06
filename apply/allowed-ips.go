package apply

import (
	"net"

	"github.com/fastcat/wirelink/autopeer"
	"github.com/fastcat/wirelink/fact"
	"github.com/fastcat/wirelink/log"
	"github.com/fastcat/wirelink/util"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type allowedIPFlag int

const (
	aipNone    allowedIPFlag = 0
	aipCurrent               = 1 << 0
	aipAdding                = 1 << 1
	aipValid                 = 1 << 2
	// aipDeleting               = 1 << 3

	aipAlreadyMask = aipCurrent | aipAdding
	// trust values others started adding, e.g. auto ip
	aipRebuildMask = aipAdding | aipValid
)

func ipNetKey(ipNet net.IPNet) string {
	return string(util.MustBytes(fact.IPNetValue{IPNet: ipNet}.MarshalBinary()))
}

func fvKey(value fact.Value) string {
	return string(util.MustBytes(value.MarshalBinary()))
}

func keyIPNet(key string) net.IPNet {
	v := &fact.IPNetValue{}
	err := v.UnmarshalBinary([]byte(key))
	// this should never happen
	if err != nil {
		panic(err)
	}
	return v.IPNet
}

// EnsureAllowedIPs updates the device config if needed to add all the
// AllowedIPs from the facts to the peer. This assumes that facts have already
// been filtered to be just the trusted ones.
func EnsureAllowedIPs(
	peer *wgtypes.Peer,
	facts []*fact.Fact,
	cfg *wgtypes.PeerConfig,
	allowDeconfigure bool,
) *wgtypes.PeerConfig {
	aipFlags := make(map[string]allowedIPFlag)
	for _, aip := range peer.AllowedIPs {
		aipFlags[ipNetKey(aip)] |= aipCurrent
	}
	if cfg != nil {
		for _, aip := range cfg.AllowedIPs {
			aipFlags[ipNetKey(aip)] |= aipAdding
		}
	}
	// autoaddr is always valid
	aipFlags[ipNetKey(autopeer.AutoAddressNet(peer.PublicKey))] |= aipValid

	for _, f := range facts {
		switch f.Attribute {
		case fact.AttributeAllowedCidrV4:
			fallthrough
		case fact.AttributeAllowedCidrV6:
			key := fvKey(f.Value)
			aipFlags[key] |= aipValid
			if aipFlags[key]&aipAlreadyMask != aipNone {
				continue
			}
			if ipn, ok := f.Value.(*fact.IPNetValue); ok {
				if cfg == nil {
					cfg = &wgtypes.PeerConfig{PublicKey: peer.PublicKey}
				}
				cfg.AllowedIPs = append(cfg.AllowedIPs, ipn.IPNet)
				aipFlags[key] |= aipAdding
			} else {
				log.Error("AIP Fact has wrong value type: %v => %T: %v", f.Attribute, f.Value, f.Value)
			}
		}
	}

	if allowDeconfigure {
		replace := false
		for _, f := range aipFlags {
			if f&(aipCurrent|aipValid) == aipCurrent {
				// peer has a current AIP that it should not
				// we need to convert the config to a _replace_ mode
				replace = true
				break
			}
		}
		if replace {
			// rebuild
			if cfg == nil {
				cfg = &wgtypes.PeerConfig{PublicKey: peer.PublicKey}
			}
			cfg.ReplaceAllowedIPs = true
			cfg.AllowedIPs = nil
			for k, f := range aipFlags {
				// add anything that was already being added, or determined to be valid to have (present or not)
				if f&aipRebuildMask == aipNone {
					continue
				}
				ipn := keyIPNet(k)
				cfg.AllowedIPs = append(cfg.AllowedIPs, ipn)
			}
		}
	}

	return cfg
}
