package raft

import (
	"math/rand"
	"time"

	"github.com/kr/pretty"
)

// Debugging
const Debug = 2

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 1 {
		//log.Printf(format, a...)
		pretty.Logf(format+"\n", a...)
	}
	return
}

func DDPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 3 {
		//log.Printf(format, a...)
		pretty.Logf(format+"\n", a...)
	}
	return
}

func randTimeout(t time.Duration) time.Duration {
	rand.Seed(time.Now().UnixNano())
	return t + time.Duration(rand.Int63())%t
}

func resetTimer(timer *time.Timer, t time.Duration) bool {
	// 如果chan有值未清，则清理一下
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	return timer.Reset(randTimeout(t))
}
