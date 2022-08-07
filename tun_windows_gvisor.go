//go:build !no_gvisor && windows

package tun

import (
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"

	"gvisor.dev/gvisor/pkg/bufferv2"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

var _ GVisorTun = (*NativeTun)(nil)

func (t *NativeTun) NewEndpoint() (stack.LinkEndpoint, error) {
	session, err := t.adapter.StartSession(0x800000)
	if err != nil {
		return nil, err
	}
	t.session = session
	t.readWait = session.ReadWaitEvent()
	return &WintunEndpoint{tun: t}, nil
}

var _ stack.LinkEndpoint = (*WintunEndpoint)(nil)

type WintunEndpoint struct {
	tun        *NativeTun
	dispatcher stack.NetworkDispatcher
}

func (e *WintunEndpoint) MTU() uint32 {
	return e.tun.mtu
}

func (e *WintunEndpoint) MaxHeaderLength() uint16 {
	return 0
}

func (e *WintunEndpoint) LinkAddress() tcpip.LinkAddress {
	return ""
}

func (e *WintunEndpoint) Capabilities() stack.LinkEndpointCapabilities {
	return stack.CapabilityNone
}

func (e *WintunEndpoint) Attach(dispatcher stack.NetworkDispatcher) {
	if dispatcher == nil && e.dispatcher != nil {
		e.dispatcher = nil
		return
	}
	if dispatcher != nil && e.dispatcher == nil {
		e.dispatcher = dispatcher
		go e.dispatchLoop()
	}
}

func (e *WintunEndpoint) dispatchLoop() {
	_buffer := buf.StackNewSize(int(e.tun.mtu))
	defer common.KeepAlive(_buffer)
	buffer := common.Dup(_buffer)
	defer buffer.Release()
	data := buffer.FreeBytes()
	for {
		n, err := e.tun.Read(data)
		if err != nil {
			break
		}
		packet := data[:n]
		var networkProtocol tcpip.NetworkProtocolNumber
		switch header.IPVersion(packet) {
		case header.IPv4Version:
			networkProtocol = header.IPv4ProtocolNumber
		case header.IPv6Version:
			networkProtocol = header.IPv6ProtocolNumber
		default:
			e.tun.Write([][]byte{packet})
			continue
		}
		pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
			Payload:           bufferv2.MakeWithData(packet),
			IsForwardedPacket: true,
		})
		dispatcher := e.dispatcher
		if dispatcher == nil {
			pkt.DecRef()
			return
		}
		dispatcher.DeliverNetworkPacket(networkProtocol, pkt)
		pkt.DecRef()
	}
}

func (e *WintunEndpoint) IsAttached() bool {
	return e.dispatcher != nil
}

func (e *WintunEndpoint) Wait() {
}

func (e *WintunEndpoint) ARPHardwareType() header.ARPHardwareType {
	return header.ARPHardwareNone
}

func (e *WintunEndpoint) AddHeader(buffer *stack.PacketBuffer) {
}

func (e *WintunEndpoint) WritePackets(packetBufferList stack.PacketBufferList) (int, tcpip.Error) {
	var n int
	for _, packet := range packetBufferList.AsSlice() {
		_, err := e.tun.Write(packet.AsSlices())
		if err != nil {
			return n, &tcpip.ErrAborted{}
		}
		n++
	}
	return n, nil
}
