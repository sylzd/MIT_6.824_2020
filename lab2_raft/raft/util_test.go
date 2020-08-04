package raft

import (
	"fmt"
	"testing"
	"time"
)

func TestRandTimeout(t *testing.T) {
	timeout := 300 * time.Microsecond
	t1 := randTimeout(timeout)
	t2 := randTimeout(timeout)
	fmt.Println(t1, t2)
	if t1 == t2 {
		t.Error("t1,t2 can not be equal")
	}
	if t1 > 2*timeout || t2 > 2*timeout {
		t.Error("t1,t2 can not be double of timeout:", timeout)
	}
}
