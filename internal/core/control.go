package core

import (
	"encoding/json"

	"github.com/irie-hq/irie/internal/ipc"
)

// controlOps are the ops of what the user does to a session as a whole and
// to its place in the list (ipc/control.go): freeze.go, priority.go and
// order.go.
var controlOps = []string{ipc.OpFreeze, ipc.OpThaw, ipc.OpPriority, ipc.OpReorder, ipc.OpPark, ipc.OpUnpark}

// control serves the control ops; ok is false for any other op.
func (c *Core) control(op string, raw json.RawMessage) (result any, ok bool, err error) {
	switch op {
	case ipc.OpFreeze, ipc.OpThaw:
		var a ipc.FreezeArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		if a.Session == "" && !a.All {
			return nil, true, ipc.Errorf(ipc.CodeBadArgs, "%s needs a session, or all", op)
		}
		r, err := c.freezeOp(a, op == ipc.OpFreeze)
		return r, true, err
	case ipc.OpPriority:
		var a ipc.PriorityArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		return nil, true, c.setPriority(a)
	case ipc.OpReorder:
		var a ipc.ReorderArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		return nil, true, c.reorder(a)
	case ipc.OpPark, ipc.OpUnpark:
		var a ipc.ParkArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		// Only a park or unpark that changed something is a step of the
		// history.
		changed, err := c.park(a, op == ipc.OpPark)
		if err != nil || !changed {
			return nil, true, err
		}
		kind := actUnpark
		if op == ipc.OpPark {
			kind = actPark
		}
		c.recordAction(kind, a.Session, nil)
		return nil, true, nil
	}
	return nil, false, nil
}
