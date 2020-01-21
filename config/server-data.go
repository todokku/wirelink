package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/spf13/viper"

	"github.com/fastcat/wirelink/log"
	"github.com/fastcat/wirelink/trust"

	"golang.zx2c4.com/wireguard/wgctrl"
)

// ServerData represents the raw data from the config for the server,
// before it is cleaned up into a `Server` config object.
type ServerData struct {
	Iface  string
	Port   int
	Router *bool
	Chatty bool

	Peers []PeerData

	ReportIfaces []string
	HideIfaces   []string

	Debug   bool
	Dump    bool
	Help    bool
	Version bool

	// this prop is here for compat, but is ignored because it's how we find the
	// config file, so the config file can't use it to point at a different config
	configPath string `mapstructure:"config-path"`
}

// Parse converts the raw configuration data into a ready to use server config.
func (s *ServerData) Parse(vcfg *viper.Viper, wgc *wgctrl.Client) (ret *Server, err error) {
	// apply this right away
	log.SetDebug(s.Debug)

	ret = new(Server)
	ret.Iface = s.Iface
	ret.Port = s.Port
	ret.Chatty = s.Chatty

	// validate all the globs
	for _, glob := range s.ReportIfaces {
		if _, err = filepath.Match(glob, ""); err != nil {
			return nil, errors.Wrapf(err, "Bad glob in ReportIfaces config: '%s'", glob)
		}
	}
	for _, glob := range s.HideIfaces {
		if _, err = filepath.Match(glob, ""); err != nil {
			return nil, errors.Wrapf(err, "Bad glob in HideIfaces config: '%s'", glob)
		}
	}
	ret.ReportIfaces = s.ReportIfaces
	ret.HideIfaces = s.HideIfaces

	ret.Peers = make(Peers)
	for _, peerDatum := range s.Peers {
		key, peerConf, err := peerDatum.Parse()
		if err != nil {
			return nil, errors.Wrapf(err, "Cannot parse peer config from %+v", peerDatum)
		}
		ret.Peers[key] = &peerConf
		// skip this log if we're in config dump mode, so that the output is _just_ the JSON
		if !s.Dump {
			log.Info("Configured peer '%s': %s", key, &peerConf)
		}
	}

	ret.Debug = s.Debug

	if s.Router == nil {
		// autodetect if we should be in router mode or not
		ret.IsRouter, err = s.detectRouter(wgc)
		if err != nil {
			return nil, err
		}
	} else {
		ret.IsRouter = *s.Router
	}

	if s.Dump {
		all := vcfg.AllSettings()
		// don't dump cli mode args
		delete(all, DumpConfigFlag)
		delete(all, VersionFlag)
		delete(all, HelpFlag)
		// have to fix the Router setting again
		if s.Router == nil {
			delete(all, RouterFlag)
		}
		// this still leaves a few settings in the output that wouldn't _normally_
		// be there, and which might not work fully in a config file:
		// `config-path`, `debug`, and `iface` at least.
		// however the point here is more to dump the effective config than to
		// regurgitate the input
		dump, err := json.MarshalIndent(all, "", "  ")
		if err != nil {
			return nil, errors.Wrapf(err, "Unable to serialize settings to JSON")
		}
		// marshal output never has the trailing newline
		dump = append(dump, '\n')
		_, err = os.Stdout.Write(dump)
		return nil, err
	}

	return
}

func (s *ServerData) detectRouter(wgc *wgctrl.Client) (bool, error) {
	// try to auto-detect router mode
	// if there are no other routers ... then we're probably a router
	// this is pretty weak, better would be to check if our IP is within some other peer's AllowedIPs
	dev, err := wgc.Device(s.Iface)
	if err != nil {
		// in dump mode, ignore this error, we may be running unprivileged
		if s.Dump {
			return false, nil
		}
		// TODO: return this error in a format that won't cause usage to be printed
		return false, errors.Wrapf(err, "Unable to open wireguard device for interface %s", s.Iface)
	}

	otherRouters := false
	for _, p := range dev.Peers {
		if trust.IsRouter(&p) {
			log.Debug("Router autodetect: found router peer %v", p.PublicKey)
			otherRouters = true
			break
		}
	}

	return !otherRouters, nil
}
