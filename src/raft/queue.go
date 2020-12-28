package raft

import (
	"errors"
	"sync"
)

//Queue struct
type Queue struct {
	mu     sync.RWMutex
	closed bool
	ch     chan bool
}

//New Queue
func New() *Queue {
	return &Queue{ch: make(chan bool)}
}

//Enqueue sth
func (q *Queue) Enqueue(t bool) error {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return errors.New("queue is closed")
	}
	q.ch <- t
	return nil
}

//Close queue
func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	close(q.ch)
	q.closed = true
}
