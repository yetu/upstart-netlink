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
	defer Shutdown(listener, upstartController, errchan)

	for {
		select {
		case msg := <-listener.Messagechan:
			netlinkMessageReceived(msg, upstartController)

		case err := <-errchan:
			fmt.Println("Received error: %v \n", err)
		}
	}
}

func Shutdown(netlinkListener *netlink.Listener, upstartController UpstartController, errchan chan error) {
	netlinkListener.Close()
	close(errchan)
}

func netlinkMessageReceived(msg netlink.Message, upstartController UpstartController) {
	switch msg.Header.MessageType() {
	case rtnetlink.RTM_NEWLINK:
		interfaceAdded(msg, upstartController)
	case rtnetlink.RTM_DELLINK:
		interfaceRemoved(msg, upstartController)
	case rtnetlink.RTM_SETLINK:
		log.Printf("Received RTM_SETLINK")
	}
}

func interfaceRemoved(msg netlink.Message, upstartController UpstartController) {
	log.Printf("Received interface removed message")
}

func interfaceAdded(msg netlink.Message, upstartController UpstartController) {
	hdr := &link.Header{}
	rtmsg := rtnetlink.NewMessage(hdr, nil)
	err := rtmsg.UnmarshalNetlink(msg.Body)
	if err != nil {
		log.Panicf("Can't unmarshal rtnetlink message: %v", err)
		return
	}
	address, err := netlink.GetAttributeCString(rtmsg, link.IFLA_IFNAME)
	if err != nil {
		address = "Error"
	}
	log.Printf("New link[%d] with Flags: %s and name: %s and changes: %s", hdr.InterfaceIndex(),
		hdr.Flags().Strings(), address, hdr.InterfaceChanges().Strings())
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
