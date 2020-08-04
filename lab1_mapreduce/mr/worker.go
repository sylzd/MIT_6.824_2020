package mr

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/rpc"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

//
// Map functions return a slice of KeyValue.
//
type KeyValue struct {
	Key   string
	Value string
}

type worker struct {
	id   uint32
	Task *MRTask
	mu   *sync.Mutex
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

func (w *worker) putReduceTask(reduceFiles []string) bool {
	var reply bool
	if ok := call("Master.AddReduceTask", reduceFiles, &reply); !ok {
		fmt.Printf("put reduce task error,exit\n")
		os.Exit(1)
	}
	return reply
}

func (w *worker) heartbeat() bool {
	var reply bool
	w.mu.Lock()
	args := w.Task
	w.mu.Unlock()
	if args == nil {
		return false
	}
	if ok := call("Master.WorkerHeartBeat", args, &reply); !ok {
		fmt.Printf("send heartbeat error,exit\n")
		os.Exit(1)
	}
	return reply
}

func (w *worker) regWorker() uint32 {
	var args EmptyArgs
	var reply uint32
	if ok := call("Master.RegWorker", args, &reply); !ok {
		fmt.Printf("get worker id fail(maybe worker num excceed),exit\n")
		os.Exit(0)
	}
	log.Printf("worker-%d registerd", reply)
	return reply
}

func (w *worker) deRegWorker() uint32 {
	var reply uint32
	if ok := call("Master.DeRegWorker", w.id, &reply); !ok {
		log.Printf("put back worker id fail,exit\n")
		os.Exit(1)
	}
	return reply
}

func (w *worker) close() error {
	// TODO 关闭或回收一些资源,如文件句柄，worker令牌等
	w.deRegWorker()

	return nil
}

func (w *worker) getTask() *MRTask {
	args := MRArgs{}
	args.ID = w.id
	reply := MRReply{}

	if ok := call("Master.GetTask", &args, &reply); !ok {
		log.Printf("worker:%v get task fail,exit\n", w.id)
		os.Exit(1)
	}
	log.Printf("task: %+v\n", reply.Task)
	w.mu.Lock()
	w.Task = reply.Task
	w.mu.Unlock()
	return reply.Task
}

func (w *worker) setTaskState(taskID uint32, state TaskState) bool {
	args := &MRArgs{
		ID:    taskID,
		State: state,
	}
	var reply bool
	if ok := call("Master.SetTaskState", args, &reply); !ok {
		log.Printf("worker:%v set task state:%+v fail,exit\n", taskID, args.State)
	}

	return reply
}

func (w *worker) doMap(t *MRTask, mapf func(string, string) []KeyValue) ([]string, error) {
	defer func() {
		if e := recover(); e != nil {
			w.setTaskState(t.TaskID, TaskStateErr)
			log.Println(e)
			var buf [4096]byte
			n := runtime.Stack(buf[:], false)
			log.Printf("panic: %s\n", string(buf[:n]))
		}
	}()
	reduces := make([][]KeyValue, t.NReduce)
	reduceNames := []string{}
	// 1. 分词
	//lzd: 这里读文件不考虑内存，这是另一个io的话题，直接全读出来，
	content, err := ioutil.ReadFile(t.Filename)
	if err != nil {
		w.setTaskState(t.TaskID, TaskStateErr)
		log.Println(err)
		return reduceNames, err
	}
	kvs := mapf(t.Filename, string(content))
	// 2. 排序
	sort.Sort(ByKey(kvs))
	// 3. 对当前map结果分配reduce,把相同的词分配到需要reduce到一起文件
	for _, kv := range kvs {
		idx := ihash(kv.Key) % t.NReduce
		reduces[idx] = append(reduces[idx], kv)
	}
	for idx, kvss := range reduces {
		// mr-X-Y， Y相同代表最终结果会被reduce在一起
		reduceName := fmt.Sprintf("mr-%d-%d", t.TaskID, idx)
		reduceNames = append(reduceNames, reduceName)
		f, err := os.Create(reduceName)
		if err != nil {
			w.setTaskState(t.TaskID, TaskStateErr)
			log.Println(err)
			return reduceNames, err

		}
		defer f.Close()
		enc := json.NewEncoder(f)
		if err := enc.Encode(&kvss); err != nil {
			w.setTaskState(t.TaskID, TaskStateErr)
			log.Println(err)
			return reduceNames, err
		}
	}

	return reduceNames, nil
}

func (w *worker) doReduce(t *MRTask, reducef func(string, []string) string) error {
	defer func() {
		if e := recover(); e != nil {
			w.setTaskState(t.TaskID, TaskStateErr)
			log.Println(e)
			var buf [4096]byte
			n := runtime.Stack(buf[:], false)
			fmt.Printf("==> %s\n", string(buf[:n]))
		}
	}()
	split := strings.Split(t.Filename, "-")
	idx := split[len(split)-1]
	files, err := filepath.Glob(t.Filename)
	if err != nil {
		w.setTaskState(t.TaskID, TaskStateErr)
		log.Println(err)
		return err
	}
	intermediate := []KeyValue{}
	for _, filename := range files {
		f, err := os.Open(filename)
		if err != nil {
			w.setTaskState(t.TaskID, TaskStateErr)
			log.Println(err)
			return err
		}
		dec := json.NewDecoder(f)
		kvs := []KeyValue{}
		if err := dec.Decode(&kvs); err != nil {
			log.Println(err)
			break
		}
		intermediate = append(intermediate, kvs...)
		f.Close()
		//os.Remove(filename)
	}
	sort.Sort(ByKey(intermediate))
	oName := "mr-out." + idx
	oFile, _ := os.Create(oName)
	defer oFile.Close()
	i := 0
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)

		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(oFile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}
	return nil
}

//
// main/mrworker.go calls this function.
//
func Worker(
	mapf func(string, string) []KeyValue,
	reducef func(string, []string) string,
) {
	// init worker
	w := &worker{
		mu: &sync.Mutex{},
	}
	w.id = w.regWorker()
	if w.id == uint32(0) {
		log.Println("worker num excceed, exit")
		os.Exit(0)
	}
	w.mu.Lock()
	w.Task = &MRTask{
		WorkerID: w.id,
		//State:    TaskStateFinish,
	}
	w.mu.Unlock()
	go func() {
		for {
			time.Sleep(3*time.Second + time.Duration(rand.Intn(1000))*time.Microsecond)
			w.heartbeat()
		}
	}()
	defer func() {
		w.close()
		os.Exit(0)
	}()
	for {
		task := w.getTask()
		if task == nil {
			log.Println("no task to run, exist")
			return
		}
		fmt.Printf("working on %+v\n", task)
		w.setTaskState(task.TaskID, TaskStateRunning)
		switch task.Phase {
		case MapPhase:
			_, err := w.doMap(task, mapf)
			if err != nil {
				w.setTaskState(task.TaskID, TaskStateErr)
				log.Fatalf("task error: %+v\n", task)
			} else {
				w.setTaskState(task.TaskID, TaskStateFinish)
			}
		case ReducePhase:
			err := w.doReduce(task, reducef)
			if err != nil {
				w.setTaskState(task.TaskID, TaskStateErr)
				log.Fatalf("task error: %+v\n", task)
			} else {
				w.setTaskState(task.TaskID, TaskStateFinish)
			}
		}
	}
}

//
// send an RPC request to the master, wait for the response.
// usually returns true.
// returns false if something goes wrong.
//
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := masterSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	// lzd: add rpc timeout
	ch := make(chan error, 1)
	go func() { ch <- c.Call(rpcname, args, reply) }()
	select {
	case err := <-ch:
		// use err and result
		if err != nil {
			log.Println(err)
			return false
		}

	case <-time.After(10 * time.Second):
		// call timed out
		log.Printf("rpc: %v(%v) timeout: %+v\n", rpcname, args, sockname)
		return false
	}

	return true
}
