package server

import (
	"sync/atomic"

	"github.com/fastcat/wirelink/fact"
	"github.com/fastcat/wirelink/log"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func (s *LinkServer) peerName(peer wgtypes.Key) string {
	return s.config.Peers.Name(peer)
}

func (s *LinkServer) printFactsIfRequested(dev *wgtypes.Device, facts []*fact.Fact) {
	printsRequested := atomic.LoadInt32(s.printsRequested)
	if printsRequested == 0 {
		return
	}
	defer atomic.CompareAndSwapInt32(s.printsRequested, printsRequested, 0)

	localFacts, err := s.collectFacts(dev)
	if err != nil {
		log.Error("Unable to load facts: %v", err)
		// note that we do NOT kill the server in this case
		return
	}
	// safe to mutate our private localFacts, but not the shared facts we received
	facts = fact.SortedCopy(fact.MergeList(append(localFacts, facts...)))
	str := "Current facts"
	for _, fact := range facts {
		str += "\n"
		str += fact.String()
	}
	log.Info("%s", str)
}

func groupFactsByPeer(facts []*fact.Fact) map[wgtypes.Key][]*fact.Fact {
	factsByPeer := make(map[wgtypes.Key][]*fact.Fact)
	for _, f := range facts {
		ps, ok := f.Subject.(*fact.PeerSubject)
		if !ok {
			// WAT
			log.Error("WAT: fact subject is a %T: %v", f.Subject, f)
			continue
		}
		factsByPeer[ps.Key] = append(factsByPeer[ps.Key], f)
	}
	return factsByPeer
}