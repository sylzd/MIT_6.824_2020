package mr

import (
	"hash/fnv"
	"time"
)

func makeRangeUint32(min, max int) []uint32 {
	a := make([]uint32, max-min+1)
	for i := range a {
		a[i] = uint32(min + i)
	}
	return a
}

func inRangeUint32(elem uint32, l []uint32) bool {
	for _, v := range l {
		if v == elem {
			return true
		}
	}
	return false
}

func inTimeSpan(start, end, check time.Time) bool {
	return check.After(start) && check.Before(end)
}

//
// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
//
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}
