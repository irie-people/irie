package ipc

// Where each op's effect goes (DESIGN §4.5, "Inside and outside"). A
// reversible gate stays reversible only while nothing outside it learns
// what it did, and so does an op. What stays in Irie, undo takes back, so
// it never asks. What leaves Irie can't come back: the person's own word
// starts it, Irie asks first where their act does not show what leaves,
// and Mikael weighs it when an agent runs it (DESIGN §4.11).

// Reach is where an op's effect goes.
type Reach string

const (
	// ReachRead changes nothing.
	ReachRead Reach = "read"
	// ReachInside changes only what Irie holds: its list and its history,
	// its settings and views, and the processes it runs as it stops and
	// continues them. Undo takes it back, or the control that set it sets
	// it back, so it never asks.
	ReachInside Reach = "inside"
	// ReachInsideOut is inside, with effects that leave Irie: a thawed or
	// reopened agent carries on and signs in again. For one session it
	// does not ask; a thaw of several asks first and thaws them one every
	// 30 s (DESIGN §4.3).
	ReachInsideOut Reach = "inside, outside effects"
	// ReachOutside's effect leaves Irie: an agent reads it, it leaves the
	// machine, or it ends or changes what Irie does not own. No undo takes
	// it back.
	ReachOutside Reach = "outside"
)

// reaches is each op's reach: the farthest that any of its uses goes, as
// DESIGN §4.5's table lists them.
var reaches = map[string]Reach{
	// What only reads.
	OpHello: ReachRead, OpList: ReachRead, OpSubscribe: ReachRead, OpComplete: ReachRead,
	OpAttachInfo: ReachRead, OpConversation: ReachRead, OpScreen: ReachRead, OpDog: ReachRead,
	OpRebuilds: ReachRead, OpLearn: ReachRead, OpPerf: ReachRead, OpResult: ReachRead,
	OpRoute: ReachRead, OpReady: ReachRead, OpArch: ReachRead,

	// Steps of the history.
	OpRename: ReachInside, OpPark: ReachInside, OpUnpark: ReachInside, OpPin: ReachInside,
	OpUnpin: ReachInside, OpReorder: ReachInside, OpPriority: ReachInside, OpSpace: ReachInside,
	OpGroup: ReachInside, OpFreeze: ReachInside, OpClose: ReachInside,
	// Steps whose agents carry on and sign in again, and undo and redo,
	// which take them.
	OpThaw: ReachInsideOut, OpReopen: ReachInsideOut, OpUndo: ReachInsideOut, OpRedo: ReachInsideOut,

	// Irie's own settings, views and files, and the agents' own channel
	// into Irie.
	OpSpaces: ReachInside, OpNotepad: ReachInside, OpConfig: ReachInside, OpTheme: ReachInside,
	OpPlugins: ReachInside, OpGabriel: ReachInside, OpLocal: ReachInside, OpPreflight: ReachInside,
	OpRate: ReachInside, OpPeers: ReachInside, OpContext: ReachInside, OpShow: ReachInside,
	OpFlip: ReachInside, OpCard: ReachInside, OpPane: ReachInside, OpHook: ReachInside,
	OpStatus: ReachInside,

	// What leaves Irie.
	OpSpawn: ReachOutside, OpFork: ReachOutside, OpBench: ReachOutside, OpTool: ReachOutside,
	OpMarkets: ReachOutside, OpInput: ReachOutside, OpSend: ReachOutside, OpContinue: ReachOutside,
	OpKeys: ReachOutside, OpStopSub: ReachOutside, OpMove: ReachOutside, OpPair: ReachOutside,
	OpVPN: ReachOutside, OpAnon: ReachOutside, OpMail: ReachOutside, OpVariorum: ReachOutside,
	OpRaphael: ReachOutside, OpDock: ReachOutside, OpTrod: ReachOutside,
	// A check asks OSV.dev about the versions the locks name.
	OpLock: ReachOutside,
}

// ReachOf is where op's effect goes. An op this version does not know is
// taken to leave Irie, the safe way to be wrong.
func ReachOf(op string) Reach {
	if r, ok := reaches[op]; ok {
		return r
	}
	return ReachOutside
}
