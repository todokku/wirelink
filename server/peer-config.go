package server

import (
	"sync"
	"time"

	"github.com/fastcat/wirelink/apply"
	"github.com/fastcat/wirelink/fact"
	"github.com/fastcat/wirelink/log"
	"github.com/fastcat/wirelink/trust"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func (s *LinkServer) configurePeers(factsRefreshed <-chan []*fact.Fact) {
	defer s.wait.Done()

	peerStates := make(map[wgtypes.Key]*apply.PeerConfigState)
	psm := new(sync.Mutex)

	// the first chunk we get is usually pretty incomplete
	// avoid deconfiguring peers until we get a second chunk
	firstRefresh := false

	for newFacts := range factsRefreshed {
		dev, err := s.deviceState()
		if err != nil {
			// this probably means the interface is down
			log.Error("Unable to load device state, giving up: %v", err)
			s.onError(err)
		}

		allFacts, err := s.collectFacts(dev)
		if err != nil {
			log.Error("Unable to collect local facts, skipping peer config: %v", err)
			continue
		}
		// it's safe for us to mutate the facts list from the local device,
		// but not the one from the channel
		allFacts = fact.MergeList(append(allFacts, newFacts...))

		factsByPeer := groupFactsByPeer(allFacts)

		// trim `peerStates` to just the current peers
		psm.Lock()
		for k := range peerStates {
			if _, ok := factsByPeer[k]; !ok {
				delete(peerStates, k)
			}
		}
		psm.Unlock()

		// track which peers are known to the device, so we know which we should add
		// this assumes that the prior layer has filtered to not include facts for
		// peers we shouldn't add
		localPeers := make(map[wgtypes.Key]bool)
		removePeer := make(map[wgtypes.Key]bool)

		wg := new(sync.WaitGroup)

		updatePeer := func(peer *wgtypes.Peer) {
			fg, ok := factsByPeer[peer.PublicKey]
			if !ok {
				removePeer[peer.PublicKey] = true
				return
			}
			psm.Lock()
			ps := peerStates[peer.PublicKey]
			psm.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				// TODO: inspect returned error? it has already been logged at this point so not much to do with it
				newState, _ := s.configurePeer(ps, peer, fg, !firstRefresh)
				psm.Lock()
				peerStates[peer.PublicKey] = newState
				psm.Unlock()
			}()
		}

		for i := range dev.Peers {
			peer := &dev.Peers[i]
			localPeers[peer.PublicKey] = true
			updatePeer(peer)
		}

		// add peers for which we have trusted facts but which are not present in the local device
		for peer := range factsByPeer {
			if localPeers[peer] {
				continue
			}
			if peer == dev.PublicKey {
				// don't try to configure the local device as its own peer
				continue
			}
			// don't delete if if we're adding it
			delete(removePeer, peer)
			// have to make a fake local peer for this, thankfully this is pretty trivial
			log.Info("Adding new local peer %s", s.peerName(peer))
			updatePeer(&wgtypes.Peer{
				PublicKey: peer,
			})
		}

		wg.Add(1)
		go s.deletePeers(wg, peerStates, psm, dev, removePeer)

		wg.Wait()

		firstRefresh = false
	}
}

func (s *LinkServer) deletePeers(
	wg *sync.WaitGroup,
	peerStates map[wgtypes.Key]*apply.PeerConfigState,
	peerStatesMutex *sync.Mutex,
	dev *wgtypes.Device,
	removePeer map[wgtypes.Key]bool,
) (err error) {
	defer wg.Done()

	// only run peer deletion if we have a peer with DelPeer trust online
	// and it has been online for longer than the fact TTL so that we are
	// reasonably sure we have all the data from it ... and we are not a router
	doDelPeers := false
	if !s.config.IsRouter {
		now := time.Now()
		for pk, pc := range s.config.Peers {
			if pc.Trust == nil || *pc.Trust < trust.DelPeer {
				continue
			}
			peerStatesMutex.Lock()
			pcs, ok := peerStates[pk]
			peerStatesMutex.Unlock()
			if !ok {
				continue
			}
			if now.Sub(pcs.AliveSince()) < FactTTL {
				continue
			}
			doDelPeers = true
			break
		}
	}

	if doDelPeers {
		var cfg wgtypes.Config
		for _, peer := range dev.Peers {
			if !removePeer[peer.PublicKey] {
				continue
			}
			// never delete routers
			if trust.IsRouter(&peer) {
				continue
			}
			log.Info("Removing peer: %s", s.peerName(peer.PublicKey))
			cfg.Peers = append(cfg.Peers, wgtypes.PeerConfig{
				PublicKey: peer.PublicKey,
				Remove:    true,
			})
		}
		if len(cfg.Peers) != 0 {
			s.stateAccess.Lock()
			defer s.stateAccess.Unlock()
			err = s.ctrl.ConfigureDevice(s.config.Iface, cfg)
			if err != nil {
				log.Error("Unable to delete peers: %v", err)
			}
		}
	}

	return
}

func (s *LinkServer) configurePeer(
	inputState *apply.PeerConfigState,
	peer *wgtypes.Peer,
	facts []*fact.Fact,
	allowDeconfigure bool,
) (state *apply.PeerConfigState, err error) {
	peerName := s.peerName(peer.PublicKey)
	// alive check uses 0 for the maxTTL, as we just care whether the alive fact
	// is still valid now
	state = inputState.Update(peer, peerName, s.peerKnowledge.peerAlive(peer.PublicKey, 0))

	// TODO: make the lock window here smaller
	// only want to take the lock for the regions where we change config
	s.stateAccess.Lock()
	defer s.stateAccess.Unlock()

	var pcfg *wgtypes.PeerConfig
	logged := false

	if state.IsHealthy() {
		// don't setup the AllowedIPs until it's both healthy and alive,
		// as we don't want to start routing traffic to it if it won't accept it
		// and reciprocate
		if state.IsAlive() {
			pcfg = apply.EnsureAllowedIPs(peer, facts, pcfg)
			if pcfg != nil && len(pcfg.AllowedIPs) > 0 {
				log.Info("Adding AIPs to peer %s: %d", peerName, len(pcfg.AllowedIPs))
				logged = true
			}
		}
	} else {
		if allowDeconfigure {
			// on a router, we are the network's memory of the AllowedIPs, so we must not
			// clear them, but on leaf devices we should remove them from the peer when
			// we don't have a direct connection so that the peer is reachable through a
			// router. for much the same reason, we don't want to remove AllowedIPs from
			// routers.
			// TODO: IsRouter doesn't belong in trust
			if !s.config.IsRouter && !trust.IsRouter(peer) {
				pcfg = apply.OnlyAutoIP(peer, pcfg)
				if pcfg != nil && pcfg.ReplaceAllowedIPs {
					log.Info("Restricting peer to be IPv6-LL only: %s", peerName)
					logged = true
				}
			}
		}

		pcfg, addedAIP := apply.EnsurePeerAutoIP(peer, pcfg)
		if addedAIP {
			log.Info("Adding IPv6-LL to %s", peerName)
			logged = true
		}

		if state.TimeForNextEndpoint() {
			nextEndpoint := state.NextEndpoint(facts)
			if nextEndpoint != nil {
				log.Info("Trying EP for %s: %v", peerName, nextEndpoint)
				logged = true
				if pcfg == nil {
					pcfg = &wgtypes.PeerConfig{PublicKey: peer.PublicKey}
				}
				pcfg.Endpoint = nextEndpoint
			}
		}
	}

	if pcfg == nil {
		return
	}

	err = s.ctrl.ConfigureDevice(s.config.Iface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{*pcfg},
	})
	if err != nil {
		log.Error("Failed to configure peer %s: %+v: %v", peerName, *pcfg, err)
		return
	} else if !logged {
		log.Info("WAT: applied unknown peer config change to %s: %+v", peerName, *pcfg)
	}

	return
}
