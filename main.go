package main

import (
	"github.com/yetu/go-netlink"
	"github.com/yetu/go-netlink/rtnetlink"
	"github.com/yetu/go-netlink/rtnetlink/link"
	"github.com/yetu/go-netlink/rtnetlink/route"
	"github.com/yetu/upstart-netlink/upstartDbus"

	"fmt"
	"log"
)

type UpstartController interface {
	Emit(name string, env []string, wait bool) (err error)
}

func main() {
	upstartController, err := upstartDbus.NewController()
	if err != nil {
		log.Panicf("Can't connect to upstart via Dbus: %v", err)
		return
	}
	listener, err := netlink.NewListener(netlink.NETLINK_ROUTE)
	if err != nil {
		log.Panicf("Can't create Netlink listener: %v", err)
	}
	errchan := make(chan error)

	go listener.Start(errchan, true)
	defer shutdown(listener, upstartController, errchan)

	for {
		select {
		case msg := <-listener.Messagechan:
			netlinkMessageReceived(msg, upstartController)

		case err := <-errchan:
			fmt.Println("Received error: %v \n", err)
		}
	}
}

func shutdown(netlinkListener *netlink.Listener, upstartController UpstartController, errchan chan error) {
	log.Printf("Shutting down netlink listener")
	netlinkListener.Close()
	close(errchan)
}

func netlinkMessageReceived(msg netlink.Message, upstartController UpstartController) {
	switch msg.Header.MessageType() {
	case rtnetlink.RTM_SETLINK, rtnetlink.RTM_NEWLINK, rtnetlink.RTM_DELLINK:
		interfaceMessageReceived(msg, upstartController)
	}
}

func interfaceMessageReceived(msg netlink.Message, upstartController UpstartController) {
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
