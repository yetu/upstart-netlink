package main

import (
	"os"

	"github.com/yetu/go-netlink"
	"github.com/yetu/upstart-netlink/upstartDbus"

	"log"
	"os/signal"
	"syscall"
)

var (
	upstartController UpstartController
	netlinkListener   *netlink.Listener
	manager           *LinkManager
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
	// nlmsg, _ := netlink.NewMessage(rtnetlink.RTM_GETROUTE, netlink.NLM_F_DUMP|netlink.NLM_F_REQUEST, &link.Header{})
	manager, err = NewLinkManager(upstartController)
	if err != nil {
		log.Panicf("Can't create LinkManager: %v", err)
	}
	go manager.Start()
	select {
	case <-c:
		shutdown()
	}
}

func shutdown() {
	log.Printf("Shutting down netlink listener and upstart controller")
	manager.Close()
}
