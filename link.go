package main

/*
  Copyright (c) 2011, Abneptis LLC. All rights reserved.
  Original Author: James D. Nurmi <james@abneptis.com>

  See LICENSE for details
*/

import (
	"fmt"
	"net"

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
	copy(ip, bytes)
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
	log.Printf("Querying current devices")
	err = manager.l.Query(*nlmsg)
	if err != nil {
		return
	}
	return
}

func (manager *LinkManager) queryAddress(index uint32) (err error) {
	hdr := addr.NewHeader(0, 0, 0, 0, index)
	nlmsg, err := netlink.NewMessage(rtnetlink.RTM_GETADDR, netlink.NLM_F_DUMP|netlink.NLM_F_REQUEST, hdr)
	if err != nil {
		return
	}
	log.Printf("Querying current addresses")
	err = manager.l.Query(*nlmsg)
	if err != nil {
		return
	}
	return
}

func (manager *LinkManager) Start() {
	log.Printf("Starting LinkManager")
	errchan := make(chan error)
	go manager.l.Start(errchan)
	manager.running = true

	err := manager.queryDevices()
	if err != nil {
		log.Panicf("Something went wrong when querying devices: %v", err)
	}

	for manager.running {
		select {
		case msg := <-manager.l.Messagechan:
			manager.netlinkMessageReceived(&msg)
		case err := <-errchan:
			log.Printf("Received error from listener: %v", err)
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
			link := &LinkHandler{cache: rtmsg}
			manager.links[hdr.InterfaceIndex()] = link
			manager.interfaceChanged(rtmsg, link, true)
		} else {
			link.cache = rtmsg
			manager.interfaceChanged(rtmsg, link, false)
		}
	case rtnetlink.RTM_NEWADDR, rtnetlink.RTM_GETADDR, rtnetlink.RTM_DELADDR:
		log.Printf("Received address message")
		manager.addrReceived(msg)
	case rtnetlink.RTM_NEWROUTE, rtnetlink.RTM_GETROUTE, rtnetlink.RTM_DELROUTE:
		log.Printf("Received route message")
	case netlink.NLMSG_ERROR:
		log.Printf("Received error packet from netlink. Header: %v, Body: %v", msg.Header, msg.Body)
		errpkt := &netlink.Error{}
		ob, err := msg.MarshalNetlink()
		err = errpkt.UnmarshalNetlink(ob)
		if err != nil {
			log.Panicf("Can't unmarshall error message: %v", err)
		} else {
			log.Printf("Error code %d, message %s", errpkt.Code(), errpkt.Error())
		}
	default:
		log.Printf("Received unhandled message. Header: %v | Body: %v", msg.Header, msg.Body)
	}
}

func (manager *LinkManager) addrReceived(msg *netlink.Message) {
	rtmsg, _ := addr.ParseMessage(*msg)
	hdr, _ := rtmsg.Header.(*addr.Header)
	interfaceIndex := hdr.InterfaceIndex()
	ipAddrAttr, err := rtmsg.GetAttribute(addr.IFA_ADDRESS)
	if err != nil {
		log.Printf("Can't get IP address attribute: %v", err)
	}
	log.Printf("Received new address for index %d: %v", interfaceIndex, ipAddrAttr.Body)
}

func (manager *LinkManager) interfaceChanged(rtmsg *rtnetlink.Message, lnk *LinkHandler, added bool) {
	name := lnk.LinkName()
	var env []string = make([]string, 5)
	env = append(env, fmt.Sprintf("IFACE=%s", name))
	hdr, _ := rtmsg.Header.(*link.Header)

	if added {
		manager.queryAddress(lnk.LinkIndex())
	}

	interfaceChanged := hdr.InterfaceChanges() != 0

	if added {
		log.Printf("Emitting net-device-added for %s", name)
		interfaceChanged = true
		upstartController.Emit("net-device-added", env, false)
	}
	if interfaceChanged {
		log.Printf("Interface %s has changes", name)
		if lnk.LinkIp() != nil && lnk.LinkFlags()&link.IFF_UP == 1 {
			log.Printf("Interface %s is up and ready to be used with address %s", name, lnk.LinkIp().String())
			env = append(env, fmt.Sprintf("ADDR=%s"), lnk.LinkIp().String())
			upstartController.Emit("net-device-up", env, false)
		} else {
			log.Printf("Interface %s is not ready to be used", name)
		}
	}
}
