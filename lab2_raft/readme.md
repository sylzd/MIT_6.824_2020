

完成实验，并提取具有容错能力的raft存储引擎实现的最小实现文件。

### 0. 通过论文及github项目了解raft的大致原理及一些开源实现或应用
- 论文: https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf 
  - 尤其关注图2
  - 本次实验会实现本片论文大部分的思想
- 注意点：
  - https://thesquareplanet.com/blog/students-guide-to-raft/ TODO
  - 多线程编程的安全性`cd ../../14.sync_and_concurrency.md`
  -  
- 项目: https://raft.github.io/
- 个人关注项目：
  - consul依赖的raft: https://github.com/hashicorp/raft
  - tidb依赖的raft: https://github.com/tikv/tikv
  - etcd依赖的raft: https://github.com/etcd-io/etcd


### 1. 获取实验初始代码

```
git clone git://g.csail.mit.edu/6.824-golabs-2020 6.824
cd 6.824/src/main
cd src/raft
go test
# 实现raft/raft.go
```

### 2. Raft.go 设计简单介绍

   ```go
   // create a new Raft server instance:
   // peers是集群成员数组包括了自己
   // me 是自己在集群成员数组中的index
   rf := Make(peers, me, persister, applyCh)
   
   // start agreement on a new log entry:
   // 写入新操作到操作日志中，**不等写入成功，马上异步返回**
   rf.Start(command interface{}) (index, term, isleader)
   
   // ask a Raft for its current term, and whether it thinks it is leader
   rf.GetState() (term, isLeader)
   
   // each time a new entry is committed to the log, each Raft peer
   // should send an ApplyMsg to the service (or tester).
   // 发送最新的操作记录到集群成员的applyCh
   type ApplyMsg
   ```

   - rpc相关的一些处理在`src/labrpc`（可临时修改debug，但最终测试需要还原）中，可以处理延迟、失败、重排、拒绝等网络问题



### 3. PART - A 实验

   实现`选举` 和 `心跳`（(`AppendEntries` RPCs with no log entries)）

   

#### 实现过程中或需要注意的点

- [x] `go test -run 2A`来测试结果
- [x] 对于论文图2，需要关注1.投票RPC请求的发送与接收 2.选举相关的规则 3. 选举时leader状态变化
- [x] 在`Raft`结构体中加入图2的选举状态，需要定义1个结构体记录所有`log entry`
- [x] 填充`RequestVoteArgs`和`RequestVoteReply`结构体，和`RequestVote()`的RPC handle.让`Make()`后中有个gouroutine在发现自己检测不到leader时，能周期性的发起选举
- [x] 实现`heartbeat`:1.定义`AppendEntries` RPC struct,并周期性的发送他 2. 实现`AppendEntries`的接受Handle,可以重置周期选举的timeout，让选举不发生
- [x] 确保所有raft节点的选举发起，不同时发生，不然他们都会选自己，从而让选举失败
- [x] 测试要求：每秒的heartbeat不能超过10次 (HeartbeatTimeout >= 100+rand ms)
- [x] 测试要求：新leader的选举要求在5s内完成，即便由于某种原因(丢包、同时发起)，发生了多轮选举。
- [x] 论文`Section 5.2`建议选举的timeout周期为150~300毫秒，这样可以匹配每s的10次heartbeat，也能保证在5s内完成选举。(ElectionTimeout = 300+ rand ms)
- [x] 可能用到[rand](https://golang.org/pkg/math/rand/)库
- [x] 写周期运行代码时，用`for+sleep`的goroutine即可,不要用`time.Timer`或`time.Ticker`，这俩货很难正确使用。(Timer 比较容易表达重置，timeout采用了Timer, 用for+sleep表达interval)
- [x] 如果测试结果哪里有问题，请反复看`图2`，它包含了选举的所有逻辑
- [x] 不要忘记实现`GetState()`
- [ ] rf.Kill()可以永久关闭实例，确保他能被rf.killed()调用，不然可能看到raft实例诈尸
- [x] `DPrintf`可以帮助调试，可以输出到文件来慢慢看调试信息`go test -run 2A > out`
- [x] `Go RPC`只会发送结构体中的大写字段，`labgob`会警告你这点，不要忽略它
- [x] 解决掉所有race的提示`go test -race`



#### 测试结果

测试结果数字含义分别为`测试所花时间`、`raft的节点数`、`测试发送的RPC数`、`RPC消息的字节数`、`提交的log entry数量`

1. 初始化选举
2. 节点挂掉、重新选举

```
=== RUN   TestInitialElection2A
Test (2A): initial election ...
  ... Passed --   3.0  3   77   19516    0
--- PASS: TestInitialElection2A (3.02s)
=== RUN   TestReElection2A
Test (2A): election after network failure ...
  ... Passed --   4.9  3   91   19096    0
--- PASS: TestReElection2A (4.90s)
PASS
```



#### 经验

- 触发选举:
  1. 初始化时，大家都是follower
  2. 大家都会初始化一个半随机的ElectionTimeout
  3. 第一个ElectionTimeoutt时，发出选举RPC
- 过race检查
  - race检查相当蛋疼，尽量先用大锁，先不考虑性能问题
- timer的使用（虽然提示说不用timer，因为重置表达的原因，在周期性检查选举触发这里还是用了）
  - 一定要看Reset方法的文档，这里曾经有大量人用起来与预期不符，参见[time: Timer.Reset is not possible to use correctly #14038](https://github.com/golang/go/issues/14038)

### 4. PART - B 实验

   实现`leader`和`follower`的操作日志记录。

- [ ] 实现`Start()`, 先通过基本的测试`TestBasicAgree2B`,然后按照论文图2，写`AppendEntries`的RPC代码，完成log的发送和接收功能
- [ ] 实现选举的限制，参考论文`section 5.4.1`
- [ ] One way to fail to reach agreement in the early Lab 2B tests is to hold repeated elections even though the leader is alive. Look for bugs in election timer management, or not sending out heartbeats immediately after winning an election.（这段话，暂时没看懂，到时候结合代码看一下TODO）
- [ ] 在`for-loop`这样的重复性事件检查代码中，加入一些小停顿,如`sync.cond`,或完成一次直接睡一会儿`time.Sleep(10 * time.Millisecond)`
- [ ] 可以多重构几次自己的代码，让代码更干净，参考 [structure](https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt), [locking](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt), and [guide](https://thesquareplanet.com/blog/students-guide-to-raft/) 页面
- [ ] 运行测试`time go test -run 2B`



#### 测试结果

1. 基本日志共识机制
2. RPC请求的正确性（通过字节数检查）
3. 当其中1个follower节点挂掉，日志共识机制是否ok

```
=== RUN   TestBasicAgree2B
Test (2B): basic agreement ...
  ... Passed --   0.9  3   18    5064    3
--- PASS: TestBasicAgree2B (0.90s)
PASS
```



#### 经验

- raft层和kv层（状态机存储层）要区分开来，raft层仅处理选举和日志复制这两件事，日志完成提交之后传给applyCh，raft的工作就完了，客户端实际的请求执行还是在kv层。
- 基础实现：kv接受请求-> kv转给raft -> raft完成日志大多数提交 -> kv-leader应用日志 -> kv-leader回复客户端请求->kv-follower异步apply日志




3. 





### 4. PART - C 实验

实现`崩溃恢复`，需要需要持久化一些state状态（图2已经表明了哪些状态需要持久化）。每次变更都需要持久化这些状态`SaveRaftState()` ，并在重启的时候重新加载`ReadRaftState()`。这个在`persister.go`中实现`Persister`对象

- [ ] 实现`raft.go`中的`persist()`和`readPersist()`, 用`labgob`序列化状态，并传给`Persister`
- [ ] 把`persist()`插入到每次状态变更的地方，插一次测试一次
- [ ] 最好定时清理掉`老log`,否则可能爆内存，对后面实验有影响
- [ ] 2C的很多测试是关于服务不可用或丢失RPC请求或返回的测试
- [ ] 由于并发日志记录，可能需要优化一下`next index`的备份（参考[extended Raft paper的第7页末尾和第8页），如果要填充日志空洞，可以看下 6.824 Raft 课程
- [ ] 2A+2B+2C所有测试的时间在4分钟内（且cpu时间1分钟）是合理的
- [ ] 运行测试`go test -run 2C`

#### 测试结果

```

```
