package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"../labgob"
	"../labrpc"
)

// import "bytes"
// import "../labgob"

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()
	applyCh   chan ApplyMsg

	// Your data here (2A, 2B, 2C).
	role int //0 followers, 1 leader, 2 candidates

	// Server state
	commitIndex int
	lastApplied int

	heartBeatMu        sync.RWMutex
	heartBeatTimestamp int64 // heartBeatTime
	// heartBeatTimestampChan chan int64 //heartBeatTime channel

	// Leader state
	nextIndex  []int // each peer nextIndex
	matchIndex []int // each peer matchedIndex

	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	currentTerm int
	votedFor    int
	log         []LogEntry
}

type LogEntry struct {
	Command interface{}
	Term    int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).
	term = rf.currentTerm
	isleader = (rf.role == 1)
	//fmt.Printf("%d: GetState result, term is %d, isLeader is %v\n", rf.me, term, isleader)
	return term, isleader
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	// Your code here (2B).
	defer rf.persist()
	rf.mu.Lock()
	defer rf.mu.Unlock()

	term, isLeader := rf.GetState()
	index := len(rf.log)
	if isLeader {
		//fmt.Printf("%d: append log request from client %s\n", rf.me, toJSON(command))
		rf.log = append(rf.log, LogEntry{command, rf.currentTerm})
	}

	return index, term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.applyCh = applyCh

	// Your initialization code here (2A, 2B, 2C).
	// Server state
	rf.dead = 0
	rf.role = 0
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.heartBeatTimestamp = time.Now().UnixNano() / 1e6
	// Leader state
	rf.nextIndex = make([]int, len(peers), len(peers))
	rf.matchIndex = make([]int, len(peers), len(peers))
	for i := 0; i < len(peers); i++ {
		rf.matchIndex[i] = 0
	}
	for i := 0; i < len(peers); i++ {
		rf.nextIndex[i] = 1
	}
	// maintain state
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.log = make([]LogEntry, 1)
	rf.log[0] = LogEntry{0, 0}
	go rf.checkHeartBeat()
	go rf.TrackApplyCommitLog()

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	return rf
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	rf.mu.Lock()
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	rf.mu.Unlock()
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var votedFor int
	var log []LogEntry
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&log) != nil {
		//fmt.Printf("Error: unpersist erro")
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.log = log
	}
}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	CurrentTerm  int
	Whoimi       int
	LastLogIndex int
	LastLogTerm  int
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	CurrentTerm       int  //server term before adding
	VoteFor           bool //-1 or not me means reject
	NotLatest         bool // if reject because not latest, just wait for more time
	VotedForCandidate int  //indicate which candidate current term vote for
	// Your data here (2A).
}

type AppendLogEntriesArgs struct {
	CurrentTerm  int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendLogEntriesReply struct {
	CurrentTerm               int
	Success                   bool
	ConflictTermFirstLogIndex int
	ConflictTermLogTerm       int
	VotedFor                  int
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) poll() bool {
	//fmt.Printf("%d: start poll \n", rf.me)
	rf.mu.Lock()
	rf.currentTerm = rf.currentTerm + 1
	rf.votedFor = rf.me
	rf.role = 2
	//check log latest
	var lastLogTerm int
	var lastLogIndex int
	if len(rf.log) > 0 {
		lastLogTerm = rf.log[len(rf.log)-1].Term
		lastLogIndex = len(rf.log) - 1
	} else {
		lastLogTerm = -1
		lastLogIndex = -1
	}
	voteTerm := rf.currentTerm
	voteCommitIndex := rf.commitIndex
	voteArgs := RequestVoteArgs{voteTerm, rf.me, lastLogIndex, lastLogTerm}

	voteResultChannel := make(chan *RequestVoteReply)
	voteNumber := 1
	voteReturnNumber := 1
	rf.mu.Unlock()

	for index := range rf.peers {
		voteReply := RequestVoteReply{}
		if index != rf.me {
			go rf.sendRequestVote(index, &voteArgs, &voteReply, voteResultChannel)
		}
	}

	for result := range voteResultChannel {
		//fmt.Printf("%d: get one result (%s) \n", rf.me, toJSON(result))
		voteReturnNumber++
		if result.VoteFor {
			voteNumber++
		} else if result.NotLatest {
			//not latest log, wait for more time, 500ms 是经验性的，大概要等于candidate wait的时间
			//fmt.Printf("%d: wait 500ms more time because log not latest\n", rf.me)
			rf.updateTimestampWithValue(rf.heartBeatTimestamp+500, "VOTE-REJECT")
		}
		if rf.role == 0 {
			//fmt.Printf("%d: Turn back to follower, stop vote because new leader\n", rf.me)
			return false
		}
		if voteNumber > len(rf.peers)/2 {
			//fmt.Printf("%d: Get majority vote %d of %d servers\n", rf.me, voteNumber, len(rf.peers))
			break //declare self as leader as soon as get majority vote
		}
		if voteReturnNumber == len(rf.peers) {
			//fmt.Printf("%d: Already been return from all server\n", rf.me)
			break
		}
	}

	close(voteResultChannel)
	if voteNumber > len(rf.peers)/2 {
		rf.mu.Lock()
		rf.role = 1
		for i := 0; i < len(rf.peers); i++ {
			rf.nextIndex[i] = lastLogIndex + 1
			rf.matchIndex[i] = 0
		}
		//fmt.Printf("%d: initial leader rf log is %s", rf.me, toJSON(rf.log))
		//fmt.Printf("%d: initial leader nextIndex  to %s and matchIndex to %s\n", rf.me, toJSON(rf.nextIndex), toJSON(rf.matchIndex))
		rf.mu.Unlock()
		rf.SendInitialLeader(voteTerm, voteCommitIndex)
	}
	return true
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply, voteResultChannel chan *RequestVoteReply) bool {
	defer func() {
		if r := recover(); r != nil {
			// fmt.Println("Recovered in f", r)
		}
	}()
	//fmt.Printf("%d: request vote %s to %d\n", rf.me, toJSON(args), server)
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	if ok {
		//fmt.Printf("%d: reply vote %s from %d\n", rf.me, toJSON(reply), server)
		rf.mu.Lock()
		if !reply.VoteFor && reply.CurrentTerm > rf.currentTerm {
			//new leader
			//fmt.Printf("%d: %d return with new leader term %d, stop current leadership, back to follower!\n", rf.me, server, reply.CurrentTerm)
			rf.role = 0 // turn back because new leader start
			//bug fix
			rf.currentTerm = reply.CurrentTerm
			rf.votedFor = reply.VotedForCandidate
		}
		rf.mu.Unlock()
		voteResultChannel <- reply
		// //fmt.Printf("%d: %d send %v to channel\n", rf.me, server, reply.VoteFor)
	} else {
		//fmt.Printf("%d: rquest vote %s time out", rf.me, toJSON(args))
	}
	return ok
}

//
// RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	//fmt.Printf("%d: receive vote request %s from %d\n", rf.me, toJSON(args), args.Whoimi)
	rf.mu.Lock()

	if args.CurrentTerm < rf.currentTerm {
		//fmt.Printf("%d: reject for old term\n", rf.me)
		reply.CurrentTerm = rf.currentTerm
		reply.VoteFor = false
		reply.NotLatest = false
		reply.VotedForCandidate = rf.votedFor
		//fmt.Printf("%d: send vote reply %s to %d\n", rf.me, toJSON(reply), args.Whoimi)
		rf.mu.Unlock()
		rf.persist()
		return
	}
	if args.CurrentTerm == rf.currentTerm && rf.votedFor != -1 && rf.votedFor != args.Whoimi {
		//if vote == -1, means first election, will vote
		//else if vote != -1 && currentTerm = this.currentTerm, means this already follow one leader, rf.votedFor != whoimi means already vote for others, reject
		//fmt.Printf("%d: reject for this term because voted\n", rf.me)
		reply.CurrentTerm = rf.currentTerm
		reply.VoteFor = false
		reply.NotLatest = false
		reply.VotedForCandidate = rf.votedFor
		//fmt.Printf("%d: send vote reply %s to %d\n", rf.me, toJSON(reply), args.Whoimi)
		rf.mu.Unlock()
		rf.persist()
		return
	}
	//bug fix
	if args.CurrentTerm > rf.currentTerm {
		//fmt.Printf("%d: no matter what happen, always turn to bigger from %d to %d!\n", rf.me, rf.currentTerm, args.CurrentTerm)
		rf.currentTerm = args.CurrentTerm
		rf.role = 0
		rf.votedFor = args.Whoimi
	}
	//check log latest
	var lastLogTerm int
	var lastLogIndex int
	if len(rf.log) > 0 {
		lastLogTerm = rf.log[len(rf.log)-1].Term
		lastLogIndex = len(rf.log) - 1
	} else {
		lastLogTerm = -1
		lastLogIndex = -1
	}

	if (args.LastLogTerm > lastLogTerm) || (args.LastLogTerm == lastLogTerm && lastLogIndex <= args.LastLogIndex) {
		//fmt.Printf("%d: accept and vote for %d!\n", rf.me, args.Whoimi)
		rf.currentTerm = args.CurrentTerm
		rf.votedFor = args.Whoimi //update votedFor and currentTerm
		rf.role = 0               //convert to follower as soon as vote for others
		reply.CurrentTerm = rf.currentTerm
		reply.VoteFor = true
		reply.NotLatest = false
		reply.VotedForCandidate = rf.votedFor
		//fmt.Printf("%d: send vote reply %s to %d\n", rf.me, toJSON(reply), args.Whoimi)
		rf.mu.Unlock()
		rf.updateTimestamp("VOTE-HANDLER")
		rf.persist()
		return
	} else {
		//fmt.Printf("%d: reject for log not latest\n", rf.me)
		reply.CurrentTerm = rf.currentTerm
		reply.VoteFor = false
		reply.NotLatest = true
		reply.VotedForCandidate = rf.votedFor
		//fmt.Printf("%d: send vote reply %s to %d\n", rf.me, toJSON(reply), args.Whoimi)
		rf.mu.Unlock()
		rf.persist()
		return
	}
}

// do leader initial
func (rf *Raft) SendInitialLeader(winTerm int, winCommitIndex int) {
	//fmt.Printf("%d: Declare election win to all\n", rf.me)
	args := AppendLogEntriesArgs{winTerm, rf.me, -1, -1, nil, winCommitIndex}
	for index := range rf.peers {
		reply := AppendLogEntriesReply{}
		if rf.role == 1 {
			if index != rf.me {
				go rf.SendAppendEntries(index, &args, &reply)
			}
		} else {
			break
		}
	}
	if rf.role == 1 {
		go rf.SendHeartBeat(winTerm) //start send heatbeat
		for server := range rf.peers {
			if server != rf.me {
				go rf.TrackAppendEntries(server) //track append Entries
				go rf.TrackLastMatchIndex()      //track match Entries
			}
		}
	}
}

// do leader heartBeat
func (rf *Raft) SendHeartBeat(winTerm int) {
	//fmt.Printf("%d: Start Send heartBeat\n", rf.me)
	for !rf.killed() && rf.role == 1 {
		args := AppendLogEntriesArgs{winTerm, rf.me, -1, -1, nil, rf.commitIndex}
		//fmt.Printf("%d: Send heartBeat with term %d, leader commit index %d, current go routine number %d\n", rf.me, winTerm, rf.commitIndex, runtime.NumGoroutine())
		for index := range rf.peers {
			reply := AppendLogEntriesReply{}
			if index != rf.me {
				go rf.SendAppendEntries(index, &args, &reply)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Check HeartBeat
func (rf *Raft) checkHeartBeat() {
	for !rf.killed() {
		randomWaitDur := int64(rand.Intn(550) + 150) //time out after 250-500ms
		for true {
			curTime := time.Now().UnixNano() / 1e6
			rf.heartBeatMu.RLock()
			timestamp := rf.heartBeatTimestamp
			rf.heartBeatMu.RUnlock()
			if rf.role != 1 && (curTime-timestamp) >= randomWaitDur { //leader will never time out
				//fmt.Printf("%d: curtime: %d, see time stampe: %d, time out after %d ms\n", rf.me, curTime, timestamp, randomWaitDur)
				rf.updateTimestamp("POLL-START")
				// rf.updateTimestampWithValue(time.Now().UnixNano()/1e6+randomWaitDur*3, "POLL-START") //candidate time out after 3 roud selection to make sure at least
				go rf.poll()
				break
			}
			time.Sleep(100 * time.Millisecond) //check heart beat every 100ms
		}
	}
}

//
// append log and log copy
//
// do commit log
func (rf *Raft) TrackApplyCommitLog() {
	// //fmt.Printf("%d: Commit log\n", rf.me)
	// rf.mu.Lock()
	// defer rf.mu.Unlock()
	for !rf.killed() {
		if rf.lastApplied < rf.commitIndex {
			for index := rf.lastApplied + 1; index < len(rf.log) && index <= rf.commitIndex; index++ {
				applyMsg := ApplyMsg{true, rf.log[index].Command, index}
				rf.applyCh <- applyMsg
				//fmt.Printf("%d: commit msg %s at %d log\n", rf.me, toJSON(applyMsg), index)
				if rf.role == 1 { //TODO: need lock
					rf.UpdateStateMachine(rf.log[index].Command)
				}
			}
			rf.lastApplied = rf.commitIndex
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// do update machine state
func (rf *Raft) UpdateStateMachine(Command interface{}) {

}

// keep track last match index of followers and update commit index
func (rf *Raft) TrackLastMatchIndex() bool {
	for !rf.killed() {
		// rf.mu.Lock()
		if rf.role != 1 {
			break
		}
		middleMatchIndex := -1
		intList := make([]int, 0)
		peersNumber := len(rf.matchIndex) - 1
		// rf.mu.Unlock()
		for index, lastMatchIndex := range rf.matchIndex {
			if index != rf.me {
				intList = append(intList, lastMatchIndex)
			}
		}
		sort.Ints(intList)
		//fmt.Printf("%d: current match index list %s (not include self)\n", rf.me, toJSON(intList))
		middleMatchIndex = intList[peersNumber/2]
		//fmt.Printf("%d: middle match index is %d, current commit index is %d\n", rf.me, middleMatchIndex, rf.commitIndex)
		if middleMatchIndex >= 0 && middleMatchIndex > rf.commitIndex && rf.log[middleMatchIndex].Term == rf.currentTerm {
			//fmt.Printf("%d: update current commitLogIndex from %d to %d\n", rf.me, rf.commitIndex, middleMatchIndex)
			rf.commitIndex = middleMatchIndex
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

//keep track copy log as long as be a leader
func (rf *Raft) TrackAppendEntries(server int) bool {
	role := 1
	for !rf.killed() {
		rf.mu.Lock()
		lastLogIndex := len(rf.log) - 1
		followerNextLogIndex := rf.nextIndex[server]
		prevLogIndex := followerNextLogIndex - 1
		prevLogTerm := -1
		role = rf.role
		rf.mu.Unlock()
		if role != 1 {
			break
		}
		if prevLogIndex > -1 {
			prevLogTerm = rf.log[prevLogIndex].Term
		}
		if followerNextLogIndex <= lastLogIndex {
			//fmt.Printf("%d: follower %d last log index %d <= leader log index %d\n", rf.me, server, followerNextLogIndex, lastLogIndex)
			args := AppendLogEntriesArgs{
				rf.currentTerm, rf.me, prevLogIndex, prevLogTerm, rf.log[followerNextLogIndex : lastLogIndex+1], rf.commitIndex}
			reply := AppendLogEntriesReply{}
			ok, newLeader := rf.SendAppendEntries(server, &args, &reply)
			if !ok {
				//fmt.Printf("%d: append log request to %d time out!\n", rf.me, server)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if newLeader {
				rf.updateTimestamp("NEW-LEADER")
				break
			}
			if !reply.Success {
				//fmt.Printf("%d: log index of server %d back skip from %d to %d\n", rf.me, server, followerNextLogIndex, reply.ConflictTermFirstLogIndex)
				rf.nextIndex[server] = reply.ConflictTermFirstLogIndex //skip all conflict term
				continue
			} else {
				//fmt.Printf("%d: append log to %d sucessfully range [%d:%d]\n", rf.me, server, followerNextLogIndex, lastLogIndex)
				rf.nextIndex[server] = lastLogIndex + 1
				rf.matchIndex[server] = lastLogIndex
			}
		} else {
			time.Sleep(20 * time.Millisecond) //track latest msg every 20 millisecond
		}
	}
	return true
	// //fmt.Printf("%d: try append log %s to %d\n", rf.me, toJSON(args))
}

// send Append Log
func (rf *Raft) SendAppendEntries(server int, args *AppendLogEntriesArgs, reply *AppendLogEntriesReply) (bool, bool) {
	//fmt.Printf("%d: send entries %s to peer %d\n", rf.me, toJSON(args), server)
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	//fmt.Printf("%d: recieve entries reply %s from peer %d\n", rf.me, toJSON(reply), server)
	newLeader := false
	if ok {
		//fmt.Printf("%d: recieve entries reply %s from peer %d\n", rf.me, toJSON(reply), server)
		rf.mu.Lock()
		if !reply.Success && reply.CurrentTerm > rf.currentTerm {
			//new leader
			//fmt.Printf("%d: %d return with new leader term %d, stop current leadership, back to follower!\n", rf.me, server, reply.CurrentTerm)
			rf.role = 0 // turn back because new leader start
			rf.currentTerm = reply.CurrentTerm
			rf.votedFor = reply.VotedFor
			newLeader = true
		}
		rf.mu.Unlock()
	}
	return ok, newLeader
}

// AppendLog or Heartbeat or DeclareLeaderShip
func (rf *Raft) AppendEntries(args *AppendLogEntriesArgs, reply *AppendLogEntriesReply) {
	//fmt.Printf("%d: recieve entries %s from peer %d\n", rf.me, toJSON(args), args.LeaderId)
	defer rf.persist()
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if args.CurrentTerm < rf.currentTerm {
		//reject
		//fmt.Printf("%d: reject because currentTerm %d > %d\n", rf.me, rf.currentTerm, args.CurrentTerm)
		reply.CurrentTerm = rf.currentTerm
		reply.Success = false
		reply.VotedFor = rf.votedFor
		return
	}
	if args.CurrentTerm == rf.currentTerm && args.LeaderId != rf.votedFor {
		//fmt.Printf("%d: warn: may be double leader in a term. my vote is: %d, leader id is: %d\n", rf.me, rf.votedFor, args.LeaderId)
	}
	if args.CurrentTerm >= rf.currentTerm {
		//fmt.Printf("%d: term jump from %d to %d, leader id is %d, commitIndex is %d\n", rf.me, rf.currentTerm, args.CurrentTerm, args.LeaderId, args.LeaderCommit)
		rf.role = 0 // Turn back to follower for old leader or candidate
		rf.currentTerm = args.CurrentTerm
		rf.votedFor = args.LeaderId //vote for new leader
		if rf.log[len(rf.log)-1].Term == args.CurrentTerm {
			//if new leader has been append by this leader term
			//fmt.Printf("%d: update current commitIndex from %d to %d", rf.me, rf.commitIndex, args.LeaderCommit)
			rf.commitIndex = min(args.LeaderCommit, len(rf.log)-1) // heart beat or initial cause commit index update
		} else {
			//fmt.Printf("%d: current leader term %d <> last log entry term %d, can't update commitIndex\n", rf.me, args.CurrentTerm, rf.log[len(rf.log)-1].Term)
		}
		reply.CurrentTerm = rf.currentTerm
		reply.Success = true //maybe false when append log
	}
	//do sth appendEntries
	if args.Entries != nil {
		if args.PrevLogIndex >= len(rf.log) {
			//fmt.Printf("%d: reject because prevLogIndex %d bigger then current log index %d\n", rf.me, args.PrevLogIndex, len(rf.log)-1)
			reply.Success = false
			reply.ConflictTermFirstLogIndex = len(rf.log)
			reply.ConflictTermLogTerm = -1
		} else if args.PrevLogIndex >= 0 && rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
			reply.Success = false
			for index := args.PrevLogIndex; index >= 0; index-- {
				if index == 0 || rf.log[index-1].Term != rf.log[index].Term {
					reply.ConflictTermFirstLogIndex = index
					reply.ConflictTermLogTerm = rf.log[index].Term
					//fmt.Printf("%d: reject because find conflict term %d at range [%d:%d]\n", rf.me, reply.ConflictTermLogTerm, index, args.PrevLogIndex)
					break
				}
			}
		} else {
			//fmt.Printf("%d: append log request from %d starting at %d length is %d \n", rf.me, args.LeaderId, args.PrevLogIndex+1, len(args.Entries))
			reply.Success = true
			//fmt.Printf("%d: before append log length %d\n", rf.me, len(rf.log))
			if args.PrevLogIndex+1 > 0 {
				rf.log = rf.log[0:(args.PrevLogIndex + 1)] //cut tail
			}
			rf.log = append(rf.log, args.Entries...)
			rf.commitIndex = min(args.LeaderCommit, len(rf.log)-1) // after append new log may cause commit index update
			//fmt.Printf("%d: after append log length %d\n", rf.me, len(rf.log))
		}
	}
	rf.updateTimestamp("LEADER-HEARTBEAT")
	//fmt.Printf("%d: reply entries %s to peer %d\n", rf.me, toJSON(reply), args.LeaderId)
}

func (rf *Raft) updateTimestamp(reason string) {
	rf.heartBeatMu.Lock()
	rf.heartBeatTimestamp = time.Now().UnixNano() / 1e6
	//fmt.Printf("%d: [%s] update timestamp to %d ms\n", rf.me, reason, rf.heartBeatTimestamp)
	rf.heartBeatMu.Unlock()
}
func (rf *Raft) updateTimestampWithValue(value int64, reason string) {
	rf.heartBeatMu.Lock()
	rf.heartBeatTimestamp = value
	//fmt.Printf("%d: [%s] update timestamp to %d ms\n", rf.me, reason, rf.heartBeatTimestamp)
	rf.heartBeatMu.Unlock()
}

func toJSON(a interface{}) string {
	b, err := json.Marshal(a)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func min(a int, b int) int {
	if a > b {
		return b
	} else {
		return a
	}
}
