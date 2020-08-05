

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
- raft算法分享：[raft算法分享](./raft/raft算法分享.md)


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

- [x] 实现`Start()`, 先通过基本的测试`TestBasicAgree2B`,然后按照论文图2，写`AppendEntries`的RPC代码，完成log的发送和接收功能
- [x] 实现选举的限制，参考论文`section 5.4.1`
- [x] 测试过程中如果发现有leader或者，仍在发生选举. 可以看下electiontimer的管理机制，或者不要在选举胜利后立马发送心跳.（这里应该说的是Timer的issue问题，我没有遇到）
- [x] 在`for-loop`这样的重复性事件检查代码中，加入一些小停顿,如`sync.cond`,或完成一次直接睡一会儿`time.Sleep(10 * time.Millisecond)`(循环读取applyCh)
- [x] 可以多重构几次自己的代码，让代码更干净，参考 [structure](https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt), [locking](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt), and [guide](https://thesquareplanet.com/blog/students-guide-to-raft/) 页面 （尤其是完成一些大测试之后，可以去掉无用的代码，便于下次测试通过）
- [x] 运行测试`time go test -run 2B`



#### 测试结果

1. 基本日志共识机制
2. RPC请求的正确性（通过字节数检查）
3. 当其中1个follower节点挂掉，日志共识机制是否ok
4. 当很多个follower节点挂掉，日志共识机制是否无法工作
5. 并发测试
6. 老leader挂掉，新leader上位；新leader又挂掉，老leader回来。确保这个过程已提交日志保留，term小但未提交的日志被清除
7. 快速回滚follower上不一致的日志
8. 确保RPC请求数量没有太多

```
=== RUN   TestBasicAgree2B
Test (2B): basic agreement ...
  ... Passed --   0.9  3   15    4196    3
--- PASS: TestBasicAgree2B (0.90s)
=== RUN   TestRPCBytes2B
Test (2B): RPC byte count ...
  ... Passed --   1.7  3   46  113596   11
--- PASS: TestRPCBytes2B (1.68s)
=== RUN   TestFailAgree2B
Test (2B): agreement despite follower disconnection ...
  ... Passed --   7.2  3  148   41478    7
--- PASS: TestFailAgree2B (7.16s)
=== RUN   TestFailNoAgree2B
Test (2B): no agreement if too many followers disconnect ...
  ... Passed --   3.7  5  120   27908    3
--- PASS: TestFailNoAgree2B (3.75s)
=== RUN   TestConcurrentStarts2B
Test (2B): concurrent Start()s ...
  ... Passed --   1.2  3   24    6804    6
--- PASS: TestConcurrentStarts2B (1.15s)
=== RUN   TestRejoin2B
Test (2B): rejoin of partitioned leader ...
  ... Passed --   5.5  3   76   19733    4
--- PASS: TestRejoin2B (5.50s)
=== RUN   TestBackup2B
Test (2B): leader backs up quickly over incorrect follower logs ...
--- PASS: TestBackup2B (1.17s)
=== RUN   TestCount2B
Test (2B): RPC counts aren't too high ...
  ... Passed --   2.7  3   90   25924   12
--- PASS: TestCount2B (2.71s)
PASS
ok      _/Users/lzd/Dropbox/mi_work/proj/test/gogogo/15.distribute_6.824_2020/lab2_raft/raft    24.437s
```



#### 经验

- raft层和kv层（状态机存储层）区分：

  raft层仅处理选举和日志复制这两件事，日志完成提交之后传给applyCh，raft的工作就完了，客户端实际的请求执行还是在kv层。

- 基础实现：

  kv接受请求-> kv转给raft -> raft完成日志大多数提交 -> kv-leader应用日志 -> kv-leader回复客户端请求->kv-follower异步apply日志

- **只有leader才能SendHeartbeat**：

  重新选举不需要非leader发送SendHeartbeat。（调试了很久，发现了自己埋的坑。。。不要自己搞创造。。。）

- 关键位置埋好调试代码：

  比如每个raft节点的日志变化，这样能比较容易分析是复制的哪个阶段出了问题。这part调试难度较大(每完成1个测试，还要确保前面的测试也能通过)。

- 分清日志的term和节点的term：

  某一条的term是不会改变的，只有可能被覆写掉

- `rf.nextIndex[server]`和`rf.matchIndex[server]`：

  leader维护的follower要复制的下一条日志索引`rf.nextIndex[server]` 和 已复制的最后一条索引`rf.matchIndex[server]` 总是同步变动

- Leader发送心跳：

  leader判断要在`SendHeartbeat`外部判断（否则可能造成嵌套型死锁），判断ok后再对所有节点发送心跳。

- `Rejoin测试`：

  **已经提交的日志不可能被覆写，因为拥有所有已提交日志的follower才能成为leader(领导人完备特性)**。证明(实现过程中需要注意的点，详细证明见5.4.3)：包含未提交日志的节点S1 给 包含已提交日志节点S2 发送vote请求时，发现

  1. S2发现日志比S1新（推导链如下)
     1. leader才能提交日志+leader只能添加日志=》已提交日志一定是按term+index顺序
     2. 已提交日志一定是按term+index顺序+S2有已提交但S1没有的日志=》S2日志比S1新
  2. 拒绝S1当leader（选举限制-拒绝日志比自己旧的当leader）

  

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
