package raft

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"../labrpc"
)

//
// example RequestVote RPC arguments structure.
// fisLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

// import "bytes"
// import "../labgob"

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

// Role ...
type Role int

const (
	// 3个角色，选举中切换
	Follower = iota
	Candidate
	Leader
)

const (
	ElectionTimeout   = time.Millisecond * 500 // 选举
	RPCTimeout        = time.Millisecond * 200 // 心跳/日志追加 的RPC超时, 远小于ElectionTimeout, 不然会发生频繁选举
	HeartbeatInterval = time.Millisecond * 50  // 也可以叫HeartbeatTimeout
)

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()
	role      Role

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// Persistent sate on all servers
	term       int // the same as currentTerm: latest term server has seen (initialized to 0 on first boot, increases monotonically)
	votedFor   int // candidateId that received vote in current term (none or initialized to -1)
	logEntries []LogEntry
	applyCh    chan ApplyMsg

	// Volatile state on all servers
	commitIndex int // index of highest log entry known to be committed (initialized to 0, increases monotonically
	lastApplied int // index of highest log entry applied to state machine (initialized to 0, increases monotonically)

	// Volatile state on leaders (Reinitialized after election)
	nextIndex  []int // for each server, index of the next log entry to send to that server (initialized to leader last log index + 1)
	matchIndex []int // for each server, index of highest log entry known to be replicated on server (initialized to 0, increases monotonically)

	electionTimer *time.Timer
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	//DPrintf("GetState()")
	defer rf.mu.Unlock()

	var term int
	var isleader bool
	// Your code here (2A).
	term = rf.term
	isleader = (rf.role == Leader)
	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	term, isLeader = rf.GetState() // with lock
	rf.mu.Lock()
	//DPrintf("Start(%+v) lock", command)
	defer rf.mu.Unlock()
	_, lastIndex := rf.lastLogTermIndex()
	//DPrintf("rf:%d isleader:%+v get command: %d", rf.me, isLeader, command)

	// Your code here (2B).
	// 1. 不是leader就别管闲事了
	if !isLeader {
		return lastIndex, term, isLeader
	}
	// 2. leader 收到client发来的命令，发起一次日志共识过程,并马上返回,不用管结果
	index = lastIndex + 1
	DPrintf("rf:%d send index:%d command:%+v, commited index: %d", rf.me, index, command, rf.commitIndex)
	rf.logEntries = append(rf.logEntries, LogEntry{
		Term:    rf.term,
		Command: command,
		Index:   index,
	})
	rf.matchIndex[rf.me] = index

	return index, term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
// lzd debug: temp var
var o sync.Once

func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.term = 0
	// votedFor 初始化为-1，每一轮选举后都重置为-1
	rf.votedFor = -1
	rf.role = Follower
	// 初始化1个空日志占位索引0
	if len(rf.logEntries) == 0 {
		rf.logEntries = append(rf.logEntries, LogEntry{
			Term: 0, Index: 0,
		})
	}
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	// 初始化applyCh
	rf.applyCh = applyCh
	// 初始化nextIndex,matchIndex
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))

	rf.electionTimer = time.NewTimer(randTimeout(ElectionTimeout))

	//go func() {
	//	for {
	//		time.Sleep(10 * time.Millisecond)
	//		if rf.lastApplied < rf.commitIndex && rf.lastApplied < len(rf.logEntries)-1 {
	//			fmt.Printf("raft:%d lastApplied:%d commitIndex:%d\n", rf.me, rf.lastApplied, rf.commitIndex)
	//			rf.lastApplied++
	//			msg := ApplyMsg{
	//				CommandValid: true,
	//				Command:      rf.logEntries[rf.lastApplied].Command,
	//				CommandIndex: rf.lastApplied + 1,
	//			}
	//			rf.applyLog(msg)
	//		}
	//	}
	//}()
	// leader/follower/candidate: ElectionTimetout触发选举
	go func() {
		for {
			select {
			case <-rf.electionTimer.C:
				DPrintf("raft:%+v electiontimer timeout:%+v", rf.me, rf.electionTimer)
				succ := rf.startElection()
				if succ {
					//选举成功马上发一次心跳，结束选举
					_, isLeader := rf.GetState() // with lock
					if isLeader {
						for i, _ := range rf.peers {
							rf.SendHeartbeat(i)
						}
					}
				} else {
					// 选举失败，重置timer，进行下一场选举
					rf.mu.Lock()
					rf.changeRole(Follower)
					rf.mu.Unlock()
				}
			}
		}
	}()

	// leader: 发送心跳/日志
	go func() {
		for {
			time.Sleep(HeartbeatInterval)
			_, isLeader := rf.GetState() // with lock
			if isLeader {
				for i, _ := range rf.peers {
					rf.SendHeartbeat(i)
				}
			}
		}
	}()

	// lzd 2B debug: 查看日志状态
	go func() {
		for {
			time.Sleep(time.Second)
			DDPrintf("raft: %d, committed:%d logs: %+v", rf.me, rf.commitIndex, rf.logEntries)
		}
	}()
	//lzd 2A debug: 以下这段代码可以是TestInitialElection2A需要达到的结果，但实际上需要由选举完成
	//rf.term = 1
	//rf.role = Follower
	//o.Do(func() {
	//	fmt.Println("once?")
	//	rf.role = Leader
	//})
	//pretty.Println(rf)

	return rf
}

func (rf *Raft) changeRole(role Role) error {
	rf.role = role
	resetTimer(rf.electionTimer, ElectionTimeout)
	switch role {
	case Follower:
	case Candidate:
		rf.term++
		rf.votedFor = rf.me
	case Leader:
		DPrintf("raft: %d become new leader term:%+v logEntries:%+v", rf.me, rf.term, rf.logEntries)
		rf.votedFor = -1
		rf.nextIndex = make([]int, len(rf.peers))
		_, lastLogIndex := rf.lastLogTermIndex()
		// 初始化所有nextIndex点位为自己的最后一条日志index+1(图7中的11)
		for i := 0; i < len(rf.peers); i++ {
			rf.nextIndex[i] = lastLogIndex + 1
		}
		// 初始化自己的matchIndex点位(不知道follower的提交情况)
		rf.matchIndex = make([]int, len(rf.peers))
		rf.matchIndex[rf.me] = lastLogIndex
	default:
		return fmt.Errorf("Unknown role:%+v\n", role)
	}
	return nil
}

func (rf *Raft) lastLogTermIndex() (int, int) {
	if len(rf.logEntries) == 0 {
		return 0, 0
	}
	lastEntry := rf.logEntries[len(rf.logEntries)-1]
	return lastEntry.Term, lastEntry.Index
}
