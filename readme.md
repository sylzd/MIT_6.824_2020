# 6.824的实验代码

首页：https://pdos.csail.mit.edu/6.824/index.html


## lab1: mapreduce实现

`实现`分布式的mapreduce程序，计算单词个数。详细实验过程，可以`cd lab1_mapreduce`

### 实现过程中或需要注意的点

- 大致过程
  1. - [x] 修改 `mr/worker.go's Worker()` 向 `master` 发送RPC请求索要任务。
  2. - [x] `master` 回应 worker 的 请求RPC，返回 文件名 和 还没进行的 map 任务
  3. - [x] worker读到 返回中的 `文件名 和 还没进行的 map 任务`，类似 `mrsequential.go` 一样执行map任务、

- - [x] Map 和 Reduce  的方法 通过`.so` 的go插件形式编写和使用，修改后通过类似`go build -buildmode=plugin ../mr/*.go`一样重新生成

- - [ ] 如果workers工作在不同的机器中，可能需要类似`GFS`的文件系统

- - [x] 输出约定`mr-X-Y`,`X`是Map任务ID，`Y`是Reduce的任务ID（我这里map的ID是全局任务ID）

- - [x] 中间结果的kv对，可通过json形式保存，如:

    ```go
            // save
            enc := json.NewEncoder(file)
                for _, kv := ... {
                    err := enc.Encode(&kv)
                }
            // load
            dec := json.NewDecoder(file)
            for {
                var kv KeyValue
                if err := dec.Decode(&kv); err != nil {
                    break
                }
                kva = append(kva, kv)
        }
    ```

- - [x] worker.go中的ihash函数生成和匹配对应reduce任务的key：`reduce_key = worker.ihash(map_key)`。

- - [x] master是个并发的`rpc server`，不要忘记锁住公共资源。

- - [x] 用`go build -race` 和 `go run -race`可以检查多线程RACE现象 . `test-mr.sh`内置加了该选项。(这个很有用~)

- - [x] 用 `time.Sleep()` 或 `sync.Cond` 来控制多线程的等待同步， **直到所有map运行完，再运行reduce**。(我这里是通过`sleep+map状态检查`实现，预留了 `cond实现方式`)

- - [x] master 和 worker之间的通信，可以设置10s的timeout，超出10s没有回应，则认为worker已经死了。

- - [x] `mrapps/crash.go` 插件 可用来随机挂掉某个map或reduce任务

- - [x] 为防止读到 部分写入的文件，可以用`ioutil.TempFile`创建临时文件，并用`os.Rename`自动重命名（按自己约定命名，问题不大）

- - [x] `test-mr.sh`的测试输出在`mr-tmp`


## lab2: raft实现

详细实验过程，可以`cd lab2_raft`

### 通过论文及github项目了解raft的大致原理及一些开源实现或应用
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


### 2A实验过程或需要注意的点

实现`选举` 和 `心跳`（(`AppendEntries` RPCs with no log entries)）

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


###  2B实验过程或需要注意的点


 实现`leader`和`follower`的操作日志记录。

- [x] 实现`Start()`, 先通过基本的测试`TestBasicAgree2B`,然后按照论文图2，写`AppendEntries`的RPC代码，完成log的发送和接收功能
- [x] 实现选举的限制，参考论文`section 5.4.1`
- [x] 测试过程中如果发现有leader或者，仍在发生选举. 可以看下electiontimer的管理机制，或者不要在选举胜利后立马发送心跳.（这里应该说的是Timer的issue问题，我没有遇到）
- [x] 在`for-loop`这样的重复性事件检查代码中，加入一些小停顿,如`sync.cond`,或完成一次直接睡一会儿`time.Sleep(10 * time.Millisecond)`(循环读取applyCh)
- [x] 可以多重构几次自己的代码，让代码更干净，参考 [structure](https://pdos.csail.mit.edu/6.824/labs/raft-structure.txt), [locking](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt), and [guide](https://thesquareplanet.com/blog/students-guide-to-raft/) 页面 （尤其是完成一些大测试之后，可以去掉无用的代码，便于下次测试通过）
- [x] 运行测试`time go test -run 2B`

## 2C实验过程或需要注意的点

实现`崩溃恢复`，需要需要持久化一些state状态（图2已经表明了哪些状态需要持久化）。每次变更都需要持久化这些状态`SaveRaftState()` ，并在重启的时候重新加载`ReadRaftState()`。这个在`persister.go`中实现`Persister`对象

