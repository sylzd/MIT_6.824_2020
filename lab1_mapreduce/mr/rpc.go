package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

import (
	"os"
	"strconv"
	"time"
)

type MRPhase int

const (
	MapPhase = iota
	ReducePhase
)

type TaskState int

const (
	// 初始状态
	TaskStateQueue = iota
	TaskStateSending
	TaskStateRunning
	TaskStateFinish
	TaskStateErr
)

//
// example to show how to declare the arguments
// and reply for an RPC.
//

type MRTask struct {
	WorkerID uint32
	TaskID   uint32
	NReduce  int
	Phase    MRPhase
	Filename string
	State    TaskState
	BeatTime time.Time
}

type EmptyArgs struct{}

type MRArgs struct {
	ID    uint32
	State TaskState
}

type MRReply struct {
	Task *MRTask
}

// Add your RPC definitions here.

// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the master.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func masterSock() string {
	s := "/var/tmp/824-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}
