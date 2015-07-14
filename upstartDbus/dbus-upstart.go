package upstartDbus

import (
	"github.com/godbus/dbus"
)

type Controller struct {
	conn          *dbus.Conn
	upstartObject *dbus.Object
}

func NewController() (controller *Controller, fail error) {

	con, err := dbus.SessionBus()
	if err != nil {
		fail = err
		return
	}
	obj := con.Object("com.ubuntu.Upstart", "/com/ubuntu/Upstart")

	controller = &Controller{conn: con, upstartObject: obj}
	return
}

func (controller Controller) Emit(name string, env []string, wait bool) (err error) {
	call := controller.upstartObject.Call("com.ubuntu.Upstart0_6.EmitEvent", dbus.FlagNoReplyExpected, name, env, wait)
	if call.Err != nil {
		err = call.Err
	}
	return
}
