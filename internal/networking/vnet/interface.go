package vnet

import (
	"net"
	"sync"
)

// A SocketOwner can send packets
type SocketOwner interface {
	OutboundPacket(*Packet) bool
	DelSocket(*Socket)
}

// An Interface is any network interface, whether physical or virtual, attached
// to a Host, which can be used to send and receive Packets.
type Interface interface {
	SocketOwner
	DetachFromNetwork()
}

// BaseInterface handles the common elements of both physical and tunnel
// Interfaces
type BaseInterface struct {
	m       *sync.Mutex
	id      string
	name    string
	world   *World
	host    *Host
	addrs   map[string]net.IPNet
	sockets map[string]*Socket
	self    Interface
}

// AddAddr adds an IP address to the interface on which it can receive packets
// and from which it can send them
func (i *BaseInterface) AddAddr(a net.IPNet) {
	i.m.Lock()
	defer i.m.Unlock()
	// TODO: clone address so caller can't break it
	i.addrs[a.String()] = a
}

// AddSocket creates a new socket on the interface
func (i *BaseInterface) AddSocket(a *net.UDPAddr) *Socket {
	ret := &Socket{
		sender: i.self,
		addr:   a,
	}
	i.m.Lock()
	// TODO: validate addr is unique-ish
	i.sockets[a.String()] = ret
	i.m.Unlock()
	// TODO: goroutines to process packets
	return ret
}

// DelSocket unregisters a socket from the interface
func (i *BaseInterface) DelSocket(s *Socket) {
	i.m.Lock()
	defer i.m.Unlock()
	// TODO: verify socket matches map entry
	delete(i.sockets, s.addr.String())
}

// InboundPacket inspects the packet to see if its destination matches any
// address on the interface and any listening socket, and if so enqueues it
// for that listener
func (i *BaseInterface) InboundPacket(p *Packet) bool {
	i.m.Lock()
	var rs *Socket
	if !destinationAddrMatch(p, i.addrs) {
		// not for this interface, not implementing forwarding, so drop it
		i.m.Unlock()
		return false
	}
	rs = destinationSocket(p, i.sockets)
	h := i.host
	i.m.Unlock()

	// if we found an interface-specific socket that matched, send it there,
	// else forward it to the host to try non-specific sockets
	if rs != nil {
		return rs.InboundPacket(p)
	}
	if h != nil {
		return h.InboundPacket(p)
	}
	return false
}
