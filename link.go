package main

/*
  Copyright (c) 2011, Abneptis LLC. All rights reserved.
  Original Author: James D. Nurmi <james@abneptis.com>

  See LICENSE for details
*/

import (
	"fmt"
	"net"
	"runtime"

	"github.com/yetu/go-netlink/rtnetlink/addr"
	"github.com/yetu/go-netlink/rtnetlink/link"
)
import "github.com/yetu/go-netlink/rtnetlink"
import "github.com/yetu/go-netlink"

import "log"

type LinkHandler struct {
	cache     *rtnetlink.Message
	addresses []*net.IP
}

func (self *LinkHandler) LinkBroadcastAddress() (s []byte) {
	attr, err := self.cache.GetAttribute(link.IFLA_BROADCAST)
	if err == nil {
		s = attr.Body
	}
	return
}

func (self *LinkHandler) LinkAddress() (s []byte) {
	attr, err := self.cache.GetAttribute(link.IFLA_ADDRESS)
	if err == nil {
		s = attr.Body
	}
	return
}

func (lnk *LinkHandler) IsUp() bool {
	return lnk.LinkFlags()&link.IFF_UP == 1
}

func (self *LinkHandler) LinkIp() (ip net.IP) {
	bytes := self.LinkAddress()
	isNull := false
	for i := range bytes {
		if bytes[i] == 0x00 {
			isNull = true
			break
		}
	}
	if isNull {
		ip = nil
		return
	}
	ip = convertBytesToIp(bytes)
	return
}

func (self *LinkHandler) LinkMTU() (i uint32) {
	i, _ = netlink.GetAttributeUint32(self.cache, link.IFLA_MTU)
	return
}

func (self *LinkHandler) LinkFlags() (flags link.Flags) {
	if hdr, ok := self.cache.Header.(*link.Header); ok {
		flags = hdr.Flags()
	}
	return
}

func (self *LinkHandler) LinkIndex() (i uint32) {
	if hdr, ok := self.cache.Header.(*link.Header); ok {
		i = hdr.InterfaceIndex()
	}
	return
}

func (link *LinkHandler) IsReady() bool {
	return link.IsUp() && len(link.addresses) > 0
}

func (self *LinkHandler) LinkName() (s string) {
	s, _ = netlink.GetAttributeCString(self.cache, link.IFLA_IFNAME)
	return
}

type LinkManager struct {
	l                 *netlink.Listener
	links             map[uint32]*LinkHandler
	upstartController UpstartController
	running           bool
}

func NewLinkManager(upstartController UpstartController) (manager *LinkManager, err error) {
	listener, err := netlink.NewListener(netlink.NETLINK_ROUTE)
	if err != nil {
		log.Printf("Can't open netlink listener: %v", err)
		return
	}
	manager = &LinkManager{l: listener, links: make(map[uint32]*LinkHandler), upstartController: upstartController}
	return
}

func (manager *LinkManager) IsRunning() bool {
	return manager.running
}

func (manager *LinkManager) queryDevices() (err error) {
	nlmsg, err := netlink.NewMessage(rtnetlink.RTM_GETLINK, netlink.NLM_F_DUMP|netlink.NLM_F_REQUEST, &link.Header{})
	if err != nil {
		return
	}
	err = manager.l.Query(nlmsg)
	if err != nil {
		return
	}
	return
}

func (manager *LinkManager) queryAddress() (err error) {
	nlmsg, err := netlink.NewMessage(rtnetlink.RTM_GETADDR, netlink.NLM_F_DUMP|netlink.NLM_F_REQUEST, &addr.Header{})
	if err != nil {
		return
	}
	err = manager.l.Query(nlmsg)
	if err != nil {
		return
	}
	return
}

func (manager *LinkManager) Start() {
	log.Printf("Starting LinkManager")
	errchan := make(chan error)
	manager.l.Start(errchan)
	manager.running = true

	err := manager.queryDevices()
	if err != nil {
		log.Panicf("Something went wrong when querying devices: %v", err)
	}

	err = manager.queryAddress()
	if err != nil {
		log.Panicf("Something went wrong when querying addresses: %v", err)
	}

	for manager.running {
		select {
		case msg := <-manager.l.Messagechan:
			manager.netlinkMessageReceived(&msg)
		case err := <-errchan:
			log.Printf("Received error from listener: %v", err)
		default:
			runtime.Gosched()
		}
	}
}

func (manager *LinkManager) Close() {
	manager.l.Close()
	upstartController.Close()
	manager.running = false
}

func (manager *LinkManager) netlinkMessageReceived(msg *netlink.Message) {
	switch msg.Header.MessageType() {
	case rtnetlink.RTM_NEWLINK, rtnetlink.RTM_GETLINK:
		rtmsg, err := link.ParseMessage(*msg)
		if err != nil {
			log.Printf("Can't parse link message: %v", err)
			break
		}
		hdr, _ := rtmsg.Header.(*link.Header)
		ifIndex := hdr.InterfaceIndex()
		link := manager.links[ifIndex]
		if link == nil {
			link := &LinkHandler{cache: rtmsg, addresses: make([]*net.IP, 0, 5)}
			manager.links[hdr.InterfaceIndex()] = link
			manager.interfaceChanged(rtmsg, link, true)
		} else {
			link.cache = rtmsg
			manager.interfaceChanged(rtmsg, link, false)
		}
	case rtnetlink.RTM_DELLINK:
		manager.linkRemoved(msg)
	case rtnetlink.RTM_NEWADDR, rtnetlink.RTM_GETADDR:
		manager.addrReceived(msg)
	case rtnetlink.RTM_DELADDR:
		manager.addressRemoved(msg)
	case rtnetlink.RTM_NEWROUTE, rtnetlink.RTM_GETROUTE:
		log.Printf("Received route message")
	case netlink.NLMSG_UNSPECIFIED:
		log.Printf("Received unspecified message. Header: %v Body %v", msg.Header, msg.Body)
	case netlink.NLMSG_DONE:
		// DO nothing with this.
	}
}

func (manager *LinkManager) linkRemoved(msg *netlink.Message) {
	rtmsg, err := link.ParseMessage(*msg)
	hdr, _ := rtmsg.Header.(*link.Header)
	if err != nil {
		log.Printf("Can't unmarshall rtnetlink message: %v", err)
	}
	link := manager.links[hdr.InterfaceIndex()]
	delete(manager.links, hdr.InterfaceIndex())
	env := manager.createEmitEnv(link)
	manager.upstartController.Emit("net-device-removed", env, false)
}

func (manager *LinkManager) addressRemoved(msg *netlink.Message) {
	rtmsg, err := addr.ParseMessage(*msg)
	if err != nil {
		log.Printf("Can't parse addr message")
	}
	hdr, _ := rtmsg.Header.(*addr.Header)
	ipAddrAttr, err := rtmsg.GetAttribute(addr.IFA_ADDRESS)
	delIp := convertNlAddrToIp(&ipAddrAttr)
	link := manager.links[hdr.InterfaceIndex()]
	log.Printf("Removing address %s from interface %d", delIp.String(), hdr.InterfaceIndex())
	var i int = -1
	for i = range link.addresses {
		ip := link.addresses[i]
		if delIp.Equal(*ip) {
			break
		}
	}
	if i > -1 {
		link.addresses = append(link.addresses[:i], link.addresses[i+1:]...)
		log.Printf("Removed one address from interface %d, %d addresses left", hdr.InterfaceIndex(), len(link.addresses))

	} else {
		log.Printf("ERROR: Requested to remove unknown address %s from interface %d", delIp.String(), hdr.InterfaceIndex())
	}
	if len(link.addresses) == 0 {
		env := manager.createEmitEnv(link)
		upstartController.Emit("net-device-down", env, false)
	}
}

func (manager *LinkManager) addrReceived(msg *netlink.Message) {
	rtmsg, _ := addr.ParseMessage(*msg)
	hdr, _ := rtmsg.Header.(*addr.Header)
	interfaceIndex := hdr.InterfaceIndex()
	ipAddrAttr, err := rtmsg.GetAttribute(addr.IFA_ADDRESS)
	if err != nil {
		log.Printf("Can't get IP address attribute: %v", err)
		return
	}

	link := manager.links[interfaceIndex]
	if link == nil {
		log.Printf("Received IP address for unknown interface index %d", interfaceIndex)
		return
	}
	ip := convertNlAddrToIp(&ipAddrAttr)
	log.Printf("Received new address for index %d: %v", interfaceIndex, ip.String())
	firstAddress := len(link.addresses) == 0
	link.addresses = append(link.addresses, &ip)
	if firstAddress {
		log.Printf("Received first address, interface should now be ready to be used")
		env := manager.createEmitEnv(link)
		upstartController.Emit("net-device-up", env, false)
	}
}

func (manager *LinkManager) createEmitEnv(link *LinkHandler) (env []string) {
	env = append(env, fmt.Sprintf("IFACE=%s", link.LinkName()))
	if len(link.addresses) > 0 {
		env = append(env, fmt.Sprintf("ADDR=%s", link.addresses[0].String()))
	}
	return
}

func (manager *LinkManager) interfaceChanged(rtmsg *rtnetlink.Message, lnk *LinkHandler, added bool) {
	name := lnk.LinkName()
	env := manager.createEmitEnv(lnk)
	hdr, _ := rtmsg.Header.(*link.Header)

	interfaceChanged := hdr.InterfaceChanges() != 0

	if added {
		log.Printf("Emitting net-device-added for %s", name)
		interfaceChanged = true
		upstartController.Emit("net-device-added", env, false)
	}
	if interfaceChanged {
		log.Printf("Interface %s has changes", name)
		if lnk.LinkIp() != nil && lnk.IsUp() {
			log.Printf("Interface %s is up and ready to be used with address %s", name, lnk.LinkIp().String())
			env = append(env, fmt.Sprintf("ADDR=%s"), lnk.LinkIp().String())
			upstartController.Emit("net-device-up", env, false)
		} else if !lnk.IsUp() {
			upstartController.Emit("net-device-down", env, false)
		} else if lnk.IsUp() && len(lnk.addresses) > 0 {
			upstartController.Emit("net-device-up", env, false)
		}
	}
}
func convertBytesToIp(body []byte) (ip net.IP) {
	if len(body) == net.IPv4len {
		return net.IPv4(body[0], body[1], body[2], body[3])
	}
	ip = make(net.IP, net.IPv6len)
	copy(ip, body)
	return
}
func convertNlAddrToIp(attr *netlink.Attribute) (ip net.IP) {
	return convertBytesToIp(attr.Body)
}
