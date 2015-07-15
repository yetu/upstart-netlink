package main

import (
	"os"

	"github.com/yetu/go-netlink"
	"github.com/yetu/go-netlink/rtnetlink"
	"github.com/yetu/go-netlink/rtnetlink/link"
	"github.com/yetu/go-netlink/rtnetlink/route"
	"github.com/yetu/upstart-netlink/upstartDbus"

	"fmt"
	"log"
	"os/signal"
	"syscall"
)

var (
	upstartController UpstartController
	netlinkListener   *netlink.Listener
)

type UpstartController interface {
	Emit(name string, env []string, wait bool) (err error)
	Close()
}

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGABRT, syscall.SIGINT, syscall.SIGTERM)
	var err error
	upstartController, err = upstartDbus.NewController()
	if err != nil {
		log.Panicf("Can't connect to upstart via Dbus: %v", err)
		return
	}
	netlinkListener, err = netlink.NewListener(netlink.NETLINK_ROUTE)
	if err != nil {
		log.Panicf("Can't create Netlink listener: %v", err)
	}
	errchan := make(chan error)

	go netlinkListener.Start(errchan, true)
	defer shutdown()

	for {
		select {
		case msg := <-netlinkListener.Messagechan:
			netlinkMessageReceived(msg)

		case err := <-errchan:
			fmt.Println("Received error: %v \n", err)
		case signal := <-c:
			log.Printf("Received signal %v, shutting down", signal)
			shutdown()
			os.Exit(0)
		}
	}
}

func shutdown() {
	log.Printf("Shutting down netlink listener and upstart controller")
	upstartController.Close()
	netlinkListener.Close()
}

func netlinkMessageReceived(msg netlink.Message) {
	switch msg.Header.MessageType() {
	case rtnetlink.RTM_SETLINK, rtnetlink.RTM_NEWLINK, rtnetlink.RTM_DELLINK:
		interfaceMessageReceived(msg)
	case rtnetlink.RTM_NEWROUTE, rtnetlink.RTM_DELROUTE, rtnetlink.RTM_GETROUTE:
		routeMessageReceived(msg)
	}
}

func routeMessageReceived(msg netlink.Message) {
	rtmsg, err := route.ParseMessage(msg)
	if err != nil {
		log.Panicf("Can't parse route netlink message: %v", err)
	}
	hdr, _ := rtmsg.Header.(*route.Header)
	log.Printf("Received route update for address family %s", hdr.AddressFamily().String())
	for i := range rtmsg.Attributes {
		attr := rtmsg.Attributes[i]
		log.Printf("Attribute %s: %v", route.AttributeTypeStrings[attr.Type], attr.Body)
	}
	log.Print("------------------------------------------------------------------")
}

func interfaceMessageReceived(msg netlink.Message) {
	rtmsg, err := link.ParseMessage(msg)
	if err != nil {
		log.Panicf("Can't unmarshal rtnetlink message: %v", err)
		return
	}
	address, err := netlink.GetAttributeCString(rtmsg, link.IFLA_IFNAME)
	if err != nil {
		log.Panicf("Can't read interface name: %v", err)
		return
	}

	hdr, _ := rtmsg.Header.(*link.Header)
	if hdr.InterfaceChanges() == 0 {
		log.Printf("Interface %s has no changes, ignoring", address)
		return
	}

	flags := hdr.Flags()
	env := []string{fmt.Sprintf("IFACE=%s", address)}
	if flags&link.IFF_UP != 0 {
		log.Printf("Link %s is up", address)
		upstartController.Emit("net-device-up", env, false)
	} else {
		log.Printf("Link %s is down", address)
		upstartController.Emit("net-device-down", env, false)
	}
}

func printRouteMessage(msg netlink.Message) {
	hdr := &route.Header{}
	rtmsg := rtnetlink.NewMessage(hdr, nil)
	err := rtmsg.UnmarshalNetlink(msg.Body)
	if err != nil {
		log.Fatalf("Can't unmarshall netlink message: %v", err)
		return
	}
	log.Printf("Received new route for Address Family %s in Table %s with Flags %s\n", hdr.AddressFamily().String(), hdr.RoutingTable().String(), hdr.Flags().Strings())
	for i := range rtmsg.Attributes {
		attr := rtmsg.Attributes[i]
		log.Printf("Attribute key: %s, Attribute value: %v", route.AttributeTypeStrings[attr.Type], attr.Body)
	}
}
