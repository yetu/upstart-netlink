package upstartDbus

import (
	"log"

	"github.com/godbus/dbus"
)

type Controller struct {
	conn          *dbus.Conn
	upstartObject dbus.Object
}

func NewController() (controller *Controller, fail error) {

	con, err := dbus.SystemBusPrivate()
	if err != nil {
		fail = err
		return
	}
	err = con.Auth(nil)
	if err != nil {
		log.Panicf("Can't authenticate to Dbus: %v", err)
		fail = err
		return
	}
	obj := con.Object("com.ubuntu.Upstart", "/com/ubuntu/Upstart")

	controller = &Controller{conn: con, upstartObject: *obj}
	return
}

func (controller Controller) Emit(name string, env []string, wait bool) error {
	log.Printf("Emetting event %s with env %v", name, env)
	return controller.upstartObject.Call("com.ubuntu.Upstart0_6.EmitEvent", 0, name, env, wait).Store()
}

func (controller *Controller) Close() {
	controller.conn.Close()
}
