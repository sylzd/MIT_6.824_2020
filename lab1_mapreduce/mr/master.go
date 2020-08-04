package mr

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Master struct {
	mrPhase MRPhase
	done    bool
	//lzd: worker最好控制下数量，所以直接通过master生成固定数量的桶，然后worker取用即可
	workerIDs   []uint32
	taskSeq     uint32
	tasks       *sync.Map
	mu          *sync.Mutex
	files       []string
	reduceFiles []string
	nReduce     int
	// lzd: map阶段全部跑完，才能进入下一阶段
	phaseCond *sync.Cond
	// 通过channel分发任务，远程worker busy时，写阻塞
	taskChan        chan string
	waitTaskTimeout int
}

// Your code here -- RPC handlers for the worker to call.

//
// start a thread that listens for RPCs from worker.go
//
func (m *Master) server() {
	rpc.Register(m)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := masterSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

//
// main/mrmaster.go calls Done() periodically to find out
// if the entire job has finished.
//
// lzd: 这里感觉没必要加锁访问，只有最后一个worker，完成最后一个工作时才会更改这个值
func (m *Master) Done() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.done
}

// GenTaskID ...
// lzd:task请求量比较多，直接使用CAS无锁操作来+1，性能更好一点
// lzd: 操蛋了，这个全局ID的设计，在任务故障重试的时候就比较蛋疼了，还不如一开始直接写死任务Map。
func (m *Master) genTaskID() uint32 {
	return atomic.AddUint32(&m.taskSeq, 1)
}

// lzdTODO: 这个等待方法不是很好
func (m *Master) isTasksDone() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	done := true
	f := func(key, value interface{}) bool {
		if value.(*MRTask).State != TaskStateFinish {
			log.Printf("task:%+v running\n", value.(*MRTask))
			done = false
			return false
		}
		return true
	}
	m.tasks.Range(f)
	return done
}

// schedule ...
func (m *Master) schedule() {
	if len(m.files) == 0 {
		log.Println("empty tasks to work")
		m.done = true
	}
	for _, f := range m.files {
		log.Printf("file: %+v enqueue", f)
		m.taskChan <- f
	}
	// lzd: 等待最后一个map任务加入到TaskMap中,才能判断所有任务（因为我这里是动态生成任务)
	// TODO: 这里是个不确定因素，最好能优化一下
	time.Sleep(5 * time.Second)
	//lzd: 等待所有worker完成map
	i := 0
	for !m.isTasksDone() {
		i++
		time.Sleep(time.Second)
		if i > m.waitTaskTimeout {
			log.Println("wait map task timeout")
			os.Exit(-1)
		}
	}

	log.Println("all map task done. now starting reduce.")

	m.mu.Lock()
	m.mrPhase = ReducePhase
	m.mu.Unlock()
	// lzd: 通过PutTask完成reduce文件任务添加
	//for _, f := range m.reduceFiles {
	//	m.taskChan <- f
	//}
	// lzd: reduce任务入队
	for i := 0; i < m.nReduce; i++ {
		m.taskChan <- fmt.Sprintf("mr-*-%d", i)
	}
	close(m.taskChan)
	time.Sleep(10 * time.Second)
	//lzd: 等待所有worker完成reduce
	i = 0
	for !m.isTasksDone() {
		i++
		time.Sleep(time.Second)
		if i > m.waitTaskTimeout {
			log.Println("wait map task timeout")
			os.Exit(-1)
		}
		time.Sleep(time.Second)
	}

	log.Println("all reduce task done")
	// all done
	m.mu.Lock()
	m.done = true
	m.mu.Unlock()
}

// getWorkerID ...
// lzd: 此处通过锁来自增worker-id，同样可以通过CAS，不过既然是作业，可以多尝试点写法
func (m *Master) getWorkerID() (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.workerIDs) == 0 {
		return 0, fmt.Errorf("worker num execeed")
	}
	var id uint32
	id, m.workerIDs = m.workerIDs[0], m.workerIDs[1:]
	return id, nil
}

// TODO: 这里使用简单的令牌桶思想有点歧义，如果一个worker退出，再进入一个新worker，实际上已经不是原来的worker了，但是worker-id可能相等
// lzd: 这里直接用map[int]bool 可能更方便， anyway,不重要，懒得改~
func (m *Master) putWorkerID(id uint32) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// lzd: 防止worker还没注册就取消注册, 鲁棒性+1 (～￣▽￣)～
	if inRangeUint32(id, m.workerIDs) {
		return id, nil
	}
	m.workerIDs = append(m.workerIDs, id)
	return id, nil
}

//
// create a Master.
// main/mrmaster.go calls this function.
// nReduce is the number of reduce tasks to use.
//
func MakeMaster(files []string, nReduce int) *Master {
	// 限制最多10个worker
	workerMaxNum := 10
	if nReduce <= 0 {
		log.Printf("nReduce: %+v is not allowed\n")
		return nil
	}
	log.Printf("%+v\n", files)

	// 这里map不限制worker数，worker数代表同时进行的任务数，与总任务数没有关系，与nReduce也没关系
	m := Master{
		workerIDs:       makeRangeUint32(1, workerMaxNum),
		mu:              &sync.Mutex{},
		done:            false,
		files:           files,
		taskChan:        make(chan string),
		tasks:           &sync.Map{},
		phaseCond:       sync.NewCond(&sync.Mutex{}),
		waitTaskTimeout: 300,
		nReduce:         nReduce,
	}
	go m.schedule()
	go m.handleHeartbeat()
	m.server()

	return &m
}

func (m *Master) handleHeartbeat() {
	for {
		time.Sleep(10 * time.Second)
		f := func(key, value interface{}) bool {
			if time.Now().Sub(value.(*MRTask).BeatTime) > 10*time.Second {
				t := &MRArgs{
					ID:    value.(*MRTask).TaskID,
					State: TaskStateErr,
				}
				// worker宕机重试未完成的任务
				if value.(*MRTask).State != TaskStateFinish {
					ok := false
					err := m.SetTaskState(t, &ok)
					if err != nil {
						log.Printf("heartbeat set state error: %+v %+v", value.(*MRTask), err)
					}
				}

				var res uint32
				m.DeRegWorker(value.(*MRTask).WorkerID, &res)
				if res != value.(*MRTask).WorkerID {
					log.Printf("heartbeat dereg worker error:%+v", value.(*MRTask))
				}
			}
			return true
		}
		m.tasks.Range(f)
	}
}

//
// 分割线--------------------------------以下为RPC方法
//

func (m *Master) WorkerHeartBeat(args *MRTask, reply *bool) error {
	now := time.Now()
	log.Printf("Heartbeat: worker-%d task-%d time-%v\n", args.WorkerID, args.TaskID, now)
	if args.TaskID == 0 {
		res := true
		reply = &res
		return nil
	}
	task, ok := m.tasks.Load(args.TaskID)
	if !ok {
		return fmt.Errorf("find task:%+v error", args)
	}
	task.(*MRTask).BeatTime = now
	m.tasks.Store(args.TaskID, task.(*MRTask))
	res := true
	reply = &res
	return nil
}

func (m *Master) RegWorker(args *EmptyArgs, reply *uint32) error {
	var err error
	*reply, err = m.getWorkerID()
	return err
}

func (m *Master) DeRegWorker(workerID uint32, reply *uint32) error {
	var err error
	*reply, err = m.putWorkerID(workerID)
	return err
}

func (m *Master) AddReduceTask(reduceFiles []string, reply *bool) error {
	m.reduceFiles = append(m.reduceFiles, reduceFiles...)
	log.Printf("file: %+v enqueue", reduceFiles)
	res := true
	reply = &res
	return nil
}

func (m *Master) GetTask(args *MRArgs, reply *MRReply) error {
	// get retry task
	f := func(key, value interface{}) bool {
		if value.(*MRTask).State == TaskStateErr {
			log.Printf("task:%+v retry\n", value.(*MRTask))
			// lzd: 为线程安全，新建一块内存，而不直接对map中的元素进行修改，sync.Map只保证它提供的几个方法是线程安全的，直接赋值并不能。
			task := &MRTask{
				WorkerID: args.ID,
				TaskID:   value.(*MRTask).TaskID,
				NReduce:  value.(*MRTask).NReduce,
				Phase:    value.(*MRTask).Phase,
				Filename: value.(*MRTask).Filename,
				State:    TaskStateSending,
				BeatTime: time.Now(),
			}
			reply.Task = task
			m.tasks.Store(reply.Task.TaskID, reply.Task)
			return false
		}
		return true
	}
	m.tasks.Range(f)
	if reply.Task != nil {
		return nil
	}

	// get new task
	file, ok := <-m.taskChan
	if !ok {
		fmt.Println("all tasks sent already")
		reply.Task = nil
		return nil
	}

	log.Printf("file: %+v dequeue", file)
	reply.Task = &MRTask{
		WorkerID: args.ID,
		Phase:    m.mrPhase,
		TaskID:   m.genTaskID(),
		State:    TaskStateSending,
		Filename: file,
		NReduce:  m.nReduce,
		BeatTime: time.Now(),
	}
	log.Printf("get task %+v\n", reply.Task)

	// lzd： 此处可通过数据库等保存任务记录,简单起见都放内存里
	m.tasks.Store(reply.Task.TaskID, reply.Task)

	// lzd: 也可以通过cond实现等待任务完成
	//func waitMap(c *sync.Cond, index int) {
	//	c.L.Lock()
	//	// 加入cond等待队列，等待cond的信号，然后内部解锁, 信号到达后重新加锁再往下运行
	//	c.Wait()
	//	fmt.Println("finish:", index)
	//	c.L.Unlock()
	//}
	//	go waitMap(m.phaseCond, int(reply.Task.TaskID))

	return nil
}

func (m *Master) SetTaskState(args *MRArgs, reply *bool) error {
	fmt.Printf("Set task:%d state:%+v\n", args.ID, args.State)
	value, ok := m.tasks.Load(args.ID)
	if !ok {
		res := false
		reply = &res
		return fmt.Errorf("find task:%+v error", args.ID)
	}
	task := &MRTask{
		WorkerID: value.(*MRTask).WorkerID,
		TaskID:   value.(*MRTask).TaskID,
		NReduce:  value.(*MRTask).NReduce,
		Phase:    value.(*MRTask).Phase,
		Filename: value.(*MRTask).Filename,
		State:    args.State,
		BeatTime: time.Now(),
	}
	m.tasks.Store(args.ID, task)
	res := true
	reply = &res
	return nil
}
