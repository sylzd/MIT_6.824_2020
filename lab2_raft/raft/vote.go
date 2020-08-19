package raft

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).

	// Followers Rule 1: Respond to RPCs from candidates and leaders
	resetTimer(rf.electionTimer, ElectionTimeout)
	rf.mu.Lock()
	DPrintf("rf:%d get RequestVote(%+v) my log:%+v", rf.me, args, rf.logEntries)
	defer rf.mu.Unlock()
	defer rf.persist()

	reply.Term = rf.term
	reply.VoteGranted = false

	// All Servers rule: 大于本节点term，则重置自己为普通Follower，并term提升, 后面正常投票
	// If RPC request or response contains term T > currentTerm: set currentTerm = T, convert to follower (§5.1)
	// Candidates Rule 3: If AppendEntries RPC received from new leader: convert to follower
	if args.Term > rf.term {
		rf.changeRole(Follower)
		rf.term = args.Term
		rf.votedFor = -1
	}

	// 1. 选举限制：候选人term太小，不投
	if args.Term < rf.term {
		DPrintf("candidate:%d term too old", args.CandidateId)
		return
	}
	// 2. 选举限制：候选人日志较旧（term较小或term相同的情况下，候选人index太小）·，不投
	lastLogTerm, lastLogIndex := rf.lastLogTermIndex()
	if args.LastLogTerm < lastLogTerm || (args.LastLogTerm == lastLogTerm && args.LastLogIndex < lastLogIndex) {
		DPrintf("candidate:%d log too old", args.CandidateId)
		return
	}
	// 3. 我是leader，不投
	if rf.role == Leader {
		DPrintf("i am leader", args.CandidateId)
		return
	}

	//// 已投给其他节点，不投
	//if rf.votedFor != -1 && rf.voteFor != args.CandidateId {
	//	return
	//}

	// 2. 正常投票
	if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
		DPrintf("%d vote for %d", rf.me, args.CandidateId)
		rf.term = args.Term
		rf.votedFor = args.CandidateId
		reply.VoteGranted = true
		rf.changeRole(Follower)
	}

}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	DPrintf("raft:%d send requestvote to raft:%d", rf.me, server)
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendRequestVoteWithContext(ctx context.Context, server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	done := make(chan bool)
	go func() {
		done <- rf.sendRequestVote(server, args, reply)
	}()
	select {
	case <-ctx.Done():
		DPrintf("raft:%d send requestvote to raft:%d timeout", rf.me, server)
		return false
	case ok := <-done:
		DPrintf("raft:%d send requestvote to raft:%d. ok: %+v reply: %+v", rf.me, server, ok, reply)
		return ok
	}
}

func (rf *Raft) sendRequestVoteWithTimeout(timeout time.Duration, server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ctx, _ := context.WithTimeout(context.Background(), RPCTimeout)
	return rf.sendRequestVoteWithContext(ctx, server, args, reply)
}

func (rf *Raft) startElection() bool {
	rf.mu.Lock()
	//TODO: 锁不能加全函数，选举期间要能接收客户端请求
	DPrintf("raft:%d startElection() lock", rf.me)
	defer rf.mu.Unlock()
	defer rf.persist()
	resetTimer(rf.electionTimer, ElectionTimeout)
	if rf.role == Leader {
		return false
	}

	// 1. 变身候选人,先选一波自己
	var voteCount int32 = 1
	rf.changeRole(Candidate)
	// 2. 带上自己的选举资本,发送选举请求（term+index越新越好）
	lastLogTerm, lastLogIndex := rf.lastLogTermIndex()
	args := RequestVoteArgs{
		Term:         rf.term,
		CandidateId:  rf.me,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	wg := &sync.WaitGroup{}
	for index, _ := range rf.peers {
		if index == rf.me {
			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			reply := RequestVoteReply{}
			ok := rf.sendRequestVoteWithTimeout(RPCTimeout, index, &args, &reply)
			if !ok {
				return
			}
			// 不是最新term，就不要继续了, 先把term搞对
			if reply.Term > args.Term {
				rf.term = reply.Term
				rf.changeRole(Follower)
			}
			// 拉票成功，票数喜+1
			if reply.VoteGranted {
				atomic.AddInt32(&voteCount, 1)
			}
		}(index)
	}
	wg.Wait()
	DPrintf("election done. voteCount:%d %d", voteCount, len(rf.peers))
	// 当选成功, 结束选举
	// Candidates Rule 2: If votes received from majority of servers: become leader
	if voteCount > int32(len(rf.peers)/2) {
		rf.changeRole(Leader)
		return true
	}
	return false
}
