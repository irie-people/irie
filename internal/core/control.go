package core

import (
	"encoding/json"

	"github.com/irie-people/irie/internal/ipc"
)

// controlOps are the ops of what the user does to a session as a whole and
// to its place in the list (ipc/control.go): freeze.go, priority.go and
// order.go.
var controlOps = []string{ipc.OpFreeze, ipc.OpThaw, ipc.OpRest, ipc.OpWake, ipc.OpPriority, ipc.OpReorder, ipc.OpPark, ipc.OpUnpark, ipc.OpPin, ipc.OpUnpin}

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
		r, changed, err := c.freezeOp(a, op == ipc.OpFreeze)
		// What it froze or thawed, of what that changed, is a step of the
		// history: them all at once is one step, which names them.
		if err == nil && len(changed) > 0 {
			kind := actThaw
			if op == ipc.OpFreeze {
				kind = actFreeze
			}
			c.recordSteps(kind, changed, nil)
		}
		return r, true, err
	case ipc.OpRest, ipc.OpWake:
		// A rest or a wake is no step of the history: the conversation
		// stays either way (rest.go).
		var a ipc.RestArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		if op == ipc.OpWake {
			r, err := c.wakeOp(a)
			return r, true, err
		}
		if a.Session == "" && !a.Idle {
			return nil, true, ipc.Errorf(ipc.CodeBadArgs, "rest needs a session, or idle")
		}
		r, err := c.restOp(a)
		return r, true, err
	case ipc.OpPriority:
		var a ipc.PriorityArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		// Only a change of level is a step of the history.
		step, err := c.setPriority(a)
		if err != nil || step == nil {
			return nil, true, err
		}
		kind := actUnpromote
		if a.Level > 0 {
			kind = actPromote
		}
		c.recordAction(kind, a.Session, step)
		return nil, true, nil
	case ipc.OpReorder:
		var a ipc.ReorderArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		// Only a move that changed the order is a step of the history.
		step, err := c.reorder(a)
		if err == nil && step != nil {
			c.recordAction(actReorder, a.Session, step)
		}
		return nil, true, err
	case ipc.OpPark, ipc.OpUnpark:
		var a ipc.ParkArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		// Only a park or unpark that changed something is a step of the
		// history, which keeps the place the session left and the one it
		// went to.
		step, err := c.park(a, op == ipc.OpPark)
		if err != nil || step == nil {
			return nil, true, err
		}
		kind := actUnpark
		if op == ipc.OpPark {
			kind = actPark
		}
		c.recordAction(kind, a.Session, step)
		return nil, true, nil
	case ipc.OpPin, ipc.OpUnpin:
		var a ipc.ParkArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		// As park: only a pin or unpin that changed something is a step.
		step, err := c.pin(a, op == ipc.OpPin)
		if err != nil || step == nil {
			return nil, true, err
		}
		kind := actUnpin
		if op == ipc.OpPin {
			kind = actPin
		}
		c.recordAction(kind, a.Session, step)
		return nil, true, nil
	}
	return nil, false, nil
}
