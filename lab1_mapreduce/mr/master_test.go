package mr

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMaster(t *testing.T) {
	m := MakeMaster(nil, 0)
	for m.Done() == false {
		time.Sleep(time.Second)
	}

	time.Sleep(time.Second)
}

func TestgetWorkerID(t *testing.T) {
	t.Run("worker num excceed", func(t *testing.T) {
		workerNum := 1
		m := Master{
			workerIDs: makeRangeUint32(1, workerNum),
			mu:        &sync.Mutex{},
		}
		id, err := m.getWorkerID()
		fmt.Println("id", id, "err:", err)
		id, err = m.getWorkerID()
		fmt.Println("id", id, "err:", err)
	})

	t.Run("worker num normal", func(t *testing.T) {
		workerNum := 3
		m := Master{
			workerIDs: makeRangeUint32(1, workerNum),
			mu:        &sync.Mutex{},
		}
		fmt.Println(m.workerIDs)
		id, err := m.getWorkerID()
		fmt.Println("id", id)
		if err != nil {
			t.Error(err)
		}
	})

}
