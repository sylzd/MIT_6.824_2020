package raft

import (
	"context"
	"time"
)

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
		DPrintf("leader:%d term:%d send AppendEntries to rf:%d timeout", rf.me, rf.term, server)
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

	// 周期性波峰的削峰处理： 每发一个心跳间隔一下发下一个，防止follower太多, 并发升高
	interval := 10 * time.Microsecond
	time.Sleep(randTimeout(interval))

	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()

	// 目标是自己: 重置时间后return
	if rf.me == server {
		resetTimer(rf.electionTimer, ElectionTimeout)
		return
	}

	DPrintf("leader:%d send AppendEntries to rf:%d", rf.me, server)
	// 获取参数
	// Leader Rule 3: If last log index ≥ nextIndex for a follower: send AppendEntries RPC with log entries starting at nextIndex
	//	// • If successful: update nextIndex and matchIndex for follower (§5.3)
	//	// • If AppendEntries fails because of log inconsistency: decrement nextIndex and retry (§5.3) TODO: 这里处理稍微有点不同，直接将nextIndex回退为follwer的日志末尾，暂时没发现问题
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
	// All Servers rule: 发现自己leader过期，则重置自己为普通Follower，并term提升, 防止有老leader没跟上时代
	// If RPC request or response contains term T > currentTerm: set currentTerm = T, convert to follower (§5.1)
	if rf.term < reply.Term {
		DPrintf("rf:%d term:%d is older than %d, change term to %d and be follower", rf.me, rf.term, reply.Term, reply.Term)
		rf.term = reply.Term
		rf.changeRole(Follower)
		return
	}
	// DPrintf("leader:%+v nextidxs:%+v", rf.me, rf.nextIndex)
	// 处理follwer不一致：nextIndex退1格并重试
	if reply.NextIndex == -1 {
		rf.nextIndex[server]--
		rf.matchIndex[server] = rf.nextIndex[server] - 1
	}
	// 处理follower不一致： 直接修正nextIndex为正确值
	if reply.NextIndex != 0 && reply.NextIndex != -1 {
		rf.nextIndex[server] = reply.NextIndex
		rf.matchIndex[server] = reply.NextIndex - 1
	}

	// 成功复制，则跑一次提交测试
	if len(logs) != 0 && reply.Success {
		if args.Entries != nil && args.Entries[0].Term == rf.term {
			// 只 commit和apply 自己 term 的 index
			rf.commitLog()
		}
	}
}

func (rf *Raft) getAppendLogs(serverID int) (prevLogIndex, prevLogTerm int, res []LogEntry) {
	nextIdx := rf.nextIndex[serverID]
	lastLogTerm, lastLogIndex := rf.lastLogTermIndex()
	if lastLogIndex < nextIdx {
		// leader没有更新的log可以发送
		prevLogIndex = lastLogIndex
		prevLogTerm = lastLogTerm
		res = nil
		return
	}
	res = rf.logEntries[nextIdx:]
	DPrintf("leader:%d, term:%d, logEntries: %+v, sendEntries: %+v, toFollower:%d, nextIdx: %+v, commited: %d", rf.me, rf.term, rf.logEntries, res, serverID, nextIdx, rf.commitIndex)
	if len(res) == 0 {
		prevLogIndex = lastLogIndex
		prevLogTerm = lastLogTerm
		res = nil
		return
	}
	prevLogIndex = nextIdx - 1
	prevLogTerm = rf.logEntries[prevLogIndex].Term
	return
}

func (rf *Raft) commitLog() {
	//DPrintf("match:%+v lastCommited:%+v", rf.matchIndex, rf.commitIndex)
	// 按日志顺序逐一判断提交
	// Leader Rule 4: If there exists an N such that N > commitIndex, a majority of matchIndex[i] ≥ N, and log[N].term == currentTerm: set commitIndex = N (§5.3, §5.4).
	for i := rf.commitIndex + 1; i <= len(rf.logEntries)-1; i++ {
		agreeCount := 0
		for _, m := range rf.matchIndex {
			if m >= i {
				agreeCount++
				if agreeCount > len(rf.peers)/2 {
					rf.commitIndex = i
					DPrintf("rf:%d committed index:%+v\n", rf.me, i)
					break
				}
			}
		}
	}
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	DPrintf("rf:%d term:%d get leader:%d AppendEntries Args: %+v, my log: %+v commited:%d", rf.me, rf.term, args.LeaderId, args, rf.logEntries, rf.commitIndex)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()

	// Followers Rule 1: Respond to RPCs from candidates and leaders
	// 每次收到leader的rpc(心跳/日志)，都重置一下, 以免自己发起选举或下次选举
	resetTimer(rf.electionTimer, ElectionTimeout)

	// All Servers rule: 发现自己过期，则重置自己为普通Follower，并term提升, 防止有老follower没跟上时代
	// If RPC request or response contains term T > currentTerm: set currentTerm = T, convert to follower (§5.1)
	if args.Term > rf.term {
		rf.term = args.Term
		reply.Term = rf.term
		reply.Success = false
		DPrintf("rf:%d term:%d is older than %d, change term to %d", rf.me, rf.term, args.Term)
		rf.changeRole(Follower)
		return
	}

	// 1. 收到老leader的请求，直接拒绝
	// Reply false if term < currentTerm (§5.1)
	if args.Term < rf.term {
		reply.Term = rf.term
		reply.Success = false
		DPrintf("leader:%d term:%d is older than me:%d term:%d, reject", args.LeaderId, args.Term, rf.me, rf.term)
		return
	}

	// 2. 一致性检查(leader crash 会出现): (prevLogTerm, prevLogIndex）新日志前一条日志不匹配(index一样但term不一样),则leader[followerID].NextIndex回退一格，覆写冲突日志
	// Reply false if log doesn’t contain an entry at prevLogIndex whose term matches prevLogTerm (§5.3)
	if args.PrevLogIndex > 0 && args.PrevLogIndex <= len(rf.logEntries)-1 && rf.logEntries[args.PrevLogIndex].Term != args.PervLogTerm {
		reply.Success = false
		// 告诉leader，一致性检查没通过，删掉不匹配日志，将NextIndex-1
		rf.logEntries = rf.logEntries[:args.PrevLogIndex]
		reply.NextIndex = args.PrevLogIndex
		return
	}

	// 3 一致性检查(leader crash 会出现)：follower历史日志与新的append日志冲突，则删掉冲突日志及之后的所有日志，且leader[followerID].NextIndex回退到剩余日志的末尾
	// If an existing entry conflicts with a new one (same index but different terms), delete the existing entry and all that follow it (§5.3)
	// TODO 这里是不是要从commit后面的日志开始检查？
	for _, entry := range args.Entries {
		if entry.Index < len(rf.logEntries) && rf.logEntries[entry.Index].Term != entry.Term {
			rf.logEntries = rf.logEntries[:entry.Index]
			reply.Success = false
			reply.NextIndex = entry.Index
			return
		}
	}

	// 4. 一致性检查(leader crash 会出现)：follower日志缺失太多,则leader[followerID].NextIndex回退到日志的末尾
	// Append any new entries not already in the log
	if args.PrevLogIndex > len(rf.logEntries)-1 {
		reply.Success = false
		reply.NextIndex = len(rf.logEntries)
		return
	}

	// 5. 成功复制新日志后更新rf.commitIndex
	// If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
	rf.logEntries = append(rf.logEntries, args.Entries...)
	if len(args.Entries) != 0 {
		DPrintf("rf follower: %d, log entries: %+v", rf.me, rf.logEntries)
	}
	lastIndex := len(rf.logEntries) - 1
	reply.NextIndex = lastIndex + 1
	reply.Success = true

	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = args.LeaderCommit
		if args.LeaderCommit > lastIndex {
			rf.commitIndex = lastIndex
		}
	}

}
