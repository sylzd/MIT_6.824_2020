# 6.824 - lab1: mapreduce

完成实验，并提取mapreduce实验的最小实现文件。

1. 获取实验初始代码

```
git clone git://g.csail.mit.edu/6.824-golabs-2020 6.824
cd 6.824/src/main
```

2. 运行单机顺序mapreduce程序，计算单词个数找找感觉

```
# 1. 编译word_count插件: 由于我的目录再GOPATH下，对 import "../mrapps" 这种相对引用方式有影响，所以需要暂时把GOPATH清空
export GOPATH="";go build -buildmode=plugin -o wc.so ../mrapps/wc.go

# 2. 用写好简易顺序mapreduce程序，来计算 pg-*.txt的单词个数
rm mr-out*

# 3. 查看计算结果
more mr-out-0

```

3. `实现`分布式的mapreduce程序，计算单词个数

一个master,一个worker，两个程序共同完成mapreduce。程序间通过rpc通信

已有文件: `main/mrmaster.go`,`main/mrworker.go`
需要实现的文件: `mr/master.go`, `mr/worker.go`, `mr/rpc.go`

```
cd 6.824/src/main
export GOPATH="";go build -buildmode=plugin ../mrapps/wc.go
rm mr-out*
# 运行master
go run mrmaster.go pg-*.txt

# 在另一个窗口运行worker
go run mrworker.go wc.so

# 获得 mr-out-*后，完成测试
sh test-mr.sh
```

4. 实现过程中，需要注意或实现的点

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



5. 测试结果

```bash
cd main
bash test-mr.sh &> result.txt

去掉日志后输出如下：
*** Starting wc test.
--- wc test: PASS
*** Starting indexer test.
--- indexer test: PASS
*** Starting map parallelism test.
--- map parallelism test: PASS
*** Starting reduce parallelism test.
--- reduce parallelism test: PASS
*** Starting crash test.
--- crash test: PASS
*** PASSED ALL TESTS
```



6. 手动运行

```
cd main
# 1.运行master
export GOPATH="";go run mrmaster.go ./pg*.txt
# 2. 运行 worker-1
export GOPATH="";go build -buildmode=plugin -o wc.so ../mrapps/wc.go; go run mrworker.go wc.so

# 3.再开一个窗口运行 worker-2 
export GOPATH="";go build -buildmode=plugin -o wc.so ../mrapps/wc.go; go run mrworker.go wc.so

```



7.经验

1. 最开始设计上没有仔细根据mapreduce的特性设计，导致走了些弯路：简单的采用了生产者/消费者队列(worker-pull)的方式获取任务，导致在crash-recover和reduce这里走了些弯路（应该直接一开始根据文件和参数生成好task-map，push给worker，后面会轻松很多，包括任务等待也会简单些）。
2. 对分布式系统中的crash-recover机制处理不太熟，一般代码编写只用考虑异常处理，分布式系统还要考虑某个进程的异常退出，一般用心跳机制。
3. 对sync.map进行update操作时，要深拷贝一块内存出来修改该值，再load回去，因为sync.map只保证他提供的方法线程安全，而直接对value中的内存赋值则无法保证。
