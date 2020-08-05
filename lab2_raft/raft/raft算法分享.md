# raft算法分享

[TOC]



## 背景（为什么要学习并分享）

1. 使用广泛，几乎可以用在所有需要强一致性的分布式系统上。虽说每个工业实现都略有区别，但是整体思想基本变化不大。
   1. 已经上线的内部项目
      1. orchestrator高可用项目
      2. consul服务发现项目
   2. 相关紧密的项目
      1. tikv/pd
      2. etcd
      3. mongodb（v3.2及以上，raft-like（secondary拉取等））

2. 提高个人及团队对分布式系统的理解程度，能完成相关项目调试/调优/开发，提高服务质量。
   1. 帮助大家进一步理解内部项目
   2. 帮助大家理解分布式数据库的一些重要配置及其对业务的影响（如mongodb的读写concern等）

3. 个人实验项目

   https://github.com/sylzd/MIT_6.824_2020/tree/master/lab2_raft

   

## 学前班

1. 术语

   在了解算法过程前，需要了解的一些名词（可能有翻译不到位的地方，谅解），可以帮助理解算法过程。

   

   **角色：**

   leader, follower, candidate

   - leader处理所有客户端请求，即便follower收到请求也会重定向到leader
   - follower是普通的被动状态，和leader/candidate交互
   - candidate在选举时由发起选举的follower变成，是一个中间状态

   

   

   **状态机：state machine**

   在分布式系统中指基于复制状态机模型中的一个**负责执行状态命令，维护状态信息**的一个抽象名词，可以简化理解为高可用集群中的一个节点，这些节点通过一致的初始状态+一致的状态变化达到状态的完全一致。

   <img src="/Users/lzd/Dropbox/mi_work/proj/test/gogogo/15.distribute_6.824_2020/lab2_raft/raft/AB654777-9C7D-4D7E-AD61-700DE56B372B.png" alt="AB654777-9C7D-4D7E-AD61-700DE56B372B" style="zoom:50%;" />

   

   **任期:** term

   不依赖系统时钟的逻辑时钟

   - 每个任期都有且仅有一个leader
   - 一轮新的选举会生成一个新的任期
   - 选举失败的任期就会非常短
   - 之前的leader从灾难中恢复，会发现自己的任期小于当前任期数，主动退化为follower
   - 普通节点follwer不会接受，任期小于自己当前任期的请求

   <img src="/Users/lzd/Dropbox/mi_work/proj/test/gogogo/15.distribute_6.824_2020/lab2_raft/raft/0F64EE18-3738-4FAF-A5C1-8F961653A866.png" alt="0F64EE18-3738-4FAF-A5C1-8F961653A866" style="zoom:50%;" />

   

   **RPC:** 

   - 节点之间交流的方式
   - 最基础的有两个
   - - `RequestVote`: candidate对其他follower发起投票
     - `AppendEntries`: leader将日志复制给其他follower

   

2. 动画

   帮助理解大致过程，http://thesecretlivesofdata.com/raft/

   

   **规则**

   这几条限制性规则，贯穿代码编写的始终，先实现前者，在前者的基础上能实现后者。（5.4证明实现了3，4，5）

   1. 选举安全：每个任期最多一个leader(5.3 选举过程实现)

   1. leader只能添加日志：只有leader能且仅能添加日志，而不能删除或更新日志条目 （代码仅给leader append接口）
   2. 日志条目唯一性（日志匹配原则）：集群中如果两个日志的term和index一样，那么这两个日志条目是完全相同的 （1+2+选举限制安全机制+覆写机制）
   3. **leader完备性**：一条日志条目在任期内committed，那么这个日志条目一定会出现在后面任期的leader日志中 （1+2+3+选举限制安全机制）
   4. 状态机安全：如果一个节点应用了一条日志条目在状态机中, 所有节点都不能应用和这个日志条目的index相同但不一样的日志 （4+日志和数据不一致安全机制）

   

3. 概览（实验中需要实现的点大都在这张图里）

![2E5F875B-90F5-4D4B-A84E-B5F620A43C5B](/Users/lzd/Dropbox/mi_work/proj/test/gogogo/15.distribute_6.824_2020/lab2_raft/raft/2E5F875B-90F5-4D4B-A84E-B5F620A43C5B.png)



## 1. 选举

### 思路

leader通过心跳机制与follower保持联系，失联会触发选举。选举时，所有节点平等，自己选举的时候，也能给别人投票，直到选举结束。

### 主要逻辑

1. 初始化触发选举
   1. 初始化时，大家都是follower
   2. 大家都会初始化一个`半随机`的ElectionTimeout
   3. 第一个ElectionTimeout时，发出选举RPC

2. leader失联触发选举
   1. leader失联后，follower收不到leader的心跳（超出ElectionTimeout）
   2. 发生ElectionTimeout的节点, 发出选举RPC

3. 选举过程
   1. 触发选举后，可以给其他节点发送选举RPC请求拉票
   2. 拉完一轮票后，如果票数超过半数，则选举成功
   3. 如果一轮term过后，所有节点都没有选举成功(当ElectionTimeout恰好完全相等时可能会发生)，或选举出两个leader, 则重来，触发下一轮term选举
   
4. 处理选举RPC请求
   1. 选举者收到RPC请求，根据leader和自己的情况决定是否投票，并返回给选举者

### 实现

1. 周期性函数（leader给所有follower发送心跳）
    ``rf.SendHeartbeat(i)``

2. 周期性函数（follower周期性检查是否触发选举）
    ``rf.electionTimer``

3. 被选举者的选举过程
    ``rf.startElection()``
    1. 变身候选人,先选一波自己
    2. 带上自己的选举资本,发送选举请求（资本：term+index，越新越容易选上）
    3. 收到选举请求回复
       1. 如果有follower资本返回说资本更雄厚，那么就退出选举，回归follower，进入4.1
       2. 如果有follower同意选举，那么拉票成功，票数喜加1
    4. 选举结束
       1. 选举失败
       2. 票数完成majority,选举成功，并立刻给其他节点发送心跳，其他节点收到心跳都将直接结束选举

4. 选举者处理选举请求
    ``RequestVote``--RPC
   
   1. term+index没有自己新，则直接拒绝，并返回更新的term+index
   
   2. 自己已经投过其他节点，则拒绝重复投票
   
   3. 如果candidate的term+index合格且自己没投过票，那么就投给他，自己也不用再投了，老老实实当follower
   
      

## 2. 日志复制



### 思路

leader通过心跳机制与follower保持联系，并在心跳中带上需要Append的log，当大多数节点完成复制并返回给leader时，leader则apply该log到kv层。follower会在后面的心跳中收到最新提交的日志，并apply到自己本地的kv层。



### 主要逻辑

`TestBasicAgree2B`

0. **kv接受请求**：kv层收到客户端请求，转发给raft层

1.  **kv转给raft ：**raft层接收到kv层转发过来的客户端请求，开始执行操作`Start(command)`，并开始日志复制，并立即返回返回（index, term, isLeader）给kv层，不用管结果。
2. **raft完成日志大多数提交**：leader发送`AppendEntries`RPC请求，每次都获取新日志复制结果，直到完成日志**大多数提交**
3.  **kv-leader应用日志**：更新自己的`commitIndex`，并将`ApplyMsg{command, index}`传给一个`applyCh`, kv层从`applyCh`中读取消息后，开始执行请求，并返回给客户端 

4. **kv-follower应用日志**：follower收到下一个`AppendEntries`请求((为了减少消息来回次数))中的`LeaderCommit`(即leader的`commitIndex`，已提交日志的最大index)时，开始apply日志到自己本地的kv层，直到达到`LeaderCommit`



### 异常处理逻辑

比主逻辑复杂许多，或者说在分布式系统中，节点的故障处理也是分布式一致性协议的主逻辑之一。

1. `TestFailAgree2B`3个节点中的1个节点(非leader)挂掉后，剩下2个节点依然能通过`majority`提交日志；重连后，通过apply补偿，逐步追回。

   

2. **leader挂掉会发生不一致的情况（详细说明见图7）**。通过以下步骤能恢复一致性：

   - `AppendEntries`RPC请求中加入日志的一致性检查：
     - 检查项1：leader的 (prevLogTerm, prevLogIndex）（新日志前一条日志）与follower不匹配，则说明不一致
     - 检查项2：follower中的日志缺失太多，比prevLogIndex要小
     - 检查项3：follower中的历史日志有与leader不匹配的，则删除该不匹配日志及之后所有日志

   - 一致性修复：对应`nextIndex`回退一格（leader的nextIndex[followerID]-1），下一次leader会多发送一条历史日志，用来覆写follower的不合群或不存在的日志，通过迭代回退，最终所有日志会趋于一致。

     

   1. ``TestFailNoAgree2B``5个节点中的3个节点挂掉后(`检查项1`)
   2. `TestRejoin2B`Leader回归的情况 (`检查项2`) ：老leader挂掉，新leader上位；新leader又挂掉，老leader回来，也会造成leader挂掉
   3. `TestBackup2B`快速回退不正确的日志(`检查项3`）这里可以直接回退到不正确的日志索引reply.NextIndex = WrongEntry.Index)，比一格格回退快一点



### 实现





## 3. 灾难恢复

### 思路

### 主要逻辑

### 实现





## 参考资料



论文原文：https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf 

github专题主页：https://raft.github.io/

斯坦福6.824分布式系统课程：https://pdos.csail.mit.edu/6.824/labs/lab-raft.html

动画：http://thesecretlivesofdata.com/raft/