package raft

import (
	"context"
	"time"
)

// TODO
type LogEntry struct {
	Term    int
	Index   int
	Command interface{}
}

type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PervLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term      int
	Success   bool
	NextIndex int
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntriesWithContext(ctx context.Context, server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	done := make(chan bool)
	go func() {
		done <- rf.sendAppendEntries(server, args, reply)
	}()
	select {
	case <-ctx.Done():
		return false
	case ok := <-done:
		return ok
	}
}

// TODO 重复代码，可以抽象为RPC层
func (rf *Raft) sendAppendEntriesWithTimeout(timeout time.Duration, server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ctx, _ := context.WithTimeout(context.Background(), RPCTimeout)
	return rf.sendAppendEntriesWithContext(ctx, server, args, reply)
}

func (rf *Raft) SendHeartbeat(server int) {
	isLeader := true

	// 不是leader: 直接return
	_, isLeader = rf.GetState() // with lock
	if !isLeader {
		return
	}
	// 目标是自己: 直接return
	if rf.me == server {
		//resetTimer(rf.electionTimer, ElectionTimeout)
		return
	}
	// 周期性波峰的削峰处理： 每发一个心跳间隔一下发下一个，防止follower太多, 并发升高
	interval := 10 * time.Microsecond
	time.Sleep(randTimeout(interval))
	rf.mu.Lock()
	//DPrintf("SendHeartbeat(%d)", server)
	defer rf.mu.Unlock()

	// 获取参数
	prevLogIndex, prevLogTerm, logs := rf.getAppendLogs(server)
	args := AppendEntriesArgs{
		Term:         rf.term,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PervLogTerm:  prevLogTerm,
		Entries:      logs,
		LeaderCommit: rf.commitIndex,
	}
	reply := AppendEntriesReply{}
	rf.sendAppendEntriesWithTimeout(RPCTimeout, server, &args, &reply)
	//DPrintf("success:%+v", reply.Success)
	if reply.Success {
		// if reply.NextIndex > rf.nextIndex[serverID]
		rf.nextIndex[server] = reply.NextIndex
		rf.matchIndex[server] = reply.NextIndex - 1
		if args.Entries != nil && args.Entries[len(args.Entries)-1].Term == rf.term {
			// 只 commit和apply 自己 term 的 index
			rf.commitApplyLog()
		}
	}
}

func (rf *Raft) getAppendLogs(serverID int) (prevLogIndex, prevLogTerm int, res []LogEntry) {
	nextIdx := rf.nextIndex[serverID]
	lastLogTerm, lastLogIndex := rf.lastLogTermIndex()
	if nextIdx > lastLogIndex {
		// leader没有更新的log可以发送
		prevLogIndex = lastLogIndex
		prevLogTerm = lastLogTerm
		res = nil
		return
	}

	res = rf.logEntries[nextIdx:]
	prevLogIndex = nextIdx - 1
	prevLogTerm = res[len(res)-1].Term
	return
}

func (rf *Raft) commitApplyLog() {
	DPrintf("match:%+v", rf.matchIndex)
	// 按日志顺序逐一判断提交
	for i := rf.commitIndex + 1; i <= len(rf.logEntries); i++ {
		agreeCount := 0
		for _, m := range rf.matchIndex {
			if m >= i {
				agreeCount++
				if agreeCount > len(rf.peers)/2 {
					// 已经match了大多数, 则设为commit, 并传给applyCh
					msg := ApplyMsg{
						CommandValid: true,
						Command:      rf.logEntries[i].Command,
						CommandIndex: i,
					}
					DPrintf("commit:%+v\n", msg)
					rf.commitIndex = i
					// 日志安全提交后，则可以放心的应用到kv（或状态机）
					go func() {
						DPrintf("apply:%+v\n", msg)
						rf.applyCh <- msg
						DPrintf("applied:%+v\n", msg)
						rf.mu.Lock()
						rf.lastApplied = i
						rf.mu.Unlock()
					}()
					break
				}
			}
		}
	}
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	//DPrintf("rf:%d get AppendEntries Args: %+v, timer:%+v", rf.me, args, rf.electionTimer)
	rf.mu.Lock()
	//DPrintf("AppendEntries(%+v)", args)
	defer rf.mu.Unlock()
	// 每次收到leader的rpc(心跳/日志)，都重置一下, 以免自己发起选举或下次选举
	resetTimer(rf.electionTimer, ElectionTimeout)

	// 0. 所有server响应RPC必备: 大于本节点term，则重置自己为普通Follower，并term提升
	if args.Term > rf.term {
		rf.term = args.Term
		reply.Term = rf.term
		rf.changeRole(Follower)
	}

	// 1. 小于本节点term则不接收该log,返回false
	if args.Term < rf.term {
		reply.Term = rf.term
		reply.Success = false
		return
	}

	// 2. 成功复制新日志
	rf.logEntries = append(rf.logEntries, args.Entries...)
	_, lastIndex := rf.lastLogTermIndex()
	reply.NextIndex = lastIndex + 1
	reply.Success = true

	// 3. 应用已提交的日志
	for i := rf.lastApplied; i <= args.LeaderCommit; i++ {
		msg := ApplyMsg{
			CommandValid: true,
			Command:      rf.logEntries[i].Command,
			CommandIndex: rf.logEntries[i].Index,
		}
		go func() {
			DPrintf("apply:%+v\n", msg)
			rf.applyCh <- msg
			DPrintf("applied:%+v\n", msg)
			rf.mu.Lock()
			rf.lastApplied = i
			rf.mu.Unlock()
		}()
	}

}
