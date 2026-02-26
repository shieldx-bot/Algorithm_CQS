package main

import (
	"log"
	"sync"
)

type State string

const (
	Follower State = "follower"
	Cadidate       = "cadidate"
	Leader         = "leader"
)

type Raft struct {
	mu    sync.Mutex
	me    int
	peers []int
	state State

	currentTerm int
	voteFor     int
}
type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

var (
	clusterMu sync.RWMutex
	cluster   = map[int]*Raft{}
)

func (rf *Raft) AttempElection() {
	rf.mu.Lock()
	rf.state = Cadidate
	rf.currentTerm++
	rf.voteFor = rf.me
	term := rf.currentTerm
	log.Printf("[%d] attemping an election at term %d", rf.me, term)
	rf.mu.Unlock()

	votes := 1
	majority := len(rf.peers)/2 + 1
	var votesMu sync.Mutex
	var wg sync.WaitGroup

	for _, server := range rf.peers {
		if server == rf.me {
			continue
		}
		wg.Add(1)
		go func(servere int) {
			defer wg.Done()
			voteGranted := rf.CallRequestVote(servere)
			if voteGranted {
				votesMu.Lock()
				votes++
				votesMu.Unlock()
				log.Printf("[%d] received vote from %d at term %d", rf.me, servere, term)
			}
		}(server)
	}

	wg.Wait()
	rf.mu.Lock()
	if rf.state == Cadidate && votes >= majority {
		rf.state = Leader
		log.Printf("[%d] became leader at term %d with %d votes", rf.me, rf.currentTerm, votes)
	}
	rf.mu.Unlock()
}

func (rf *Raft) CallRequestVote(server int) bool {
	rf.mu.Lock()
	log.Printf("[%d] sending request vote to %d", rf.me, server)
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: 0,
		LastLogTerm:  0,
	}
	rf.mu.Unlock()

	reply := RequestVoteReply{}
	ok := rf.sendRequestVote(server, &args, &reply)
	if !ok {
		log.Printf("[%d] failed to send request vote to %d", rf.me, server)
		return false
	}
	if reply.VoteGranted {
		log.Printf("[%d] received vote from %d at term %d", rf.me, server, reply.Term)
	} else {
		log.Printf("[%d] received rejection from %d at term %d", rf.me, server, reply.Term)
	}
	return reply.VoteGranted
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	clusterMu.RLock()
	target, ok := cluster[server]
	clusterMu.RUnlock()
	if !ok {
		return false
	}
	target.HandleRequestVote(args, reply)
	return true
}

func (rf *Raft) HandleRequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		return
	}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = Follower
		rf.voteFor = -1
	}

	reply.Term = rf.currentTerm
	if rf.voteFor == -1 || rf.voteFor == args.CandidateId {
		rf.voteFor = args.CandidateId
		reply.VoteGranted = true
		return
	}

	reply.VoteGranted = false
}

func NewRaft(id int, peers []int) *Raft {
	rf := &Raft{
		me:          id,
		peers:       peers,
		state:       Follower,
		currentTerm: 0,
		voteFor:     -1,
	}

	clusterMu.Lock()
	cluster[id] = rf
	clusterMu.Unlock()

	return rf
}

func main() {
	peers := []int{1, 2, 3}
	node1 := NewRaft(1, peers)
	NewRaft(2, peers)
	NewRaft(3, peers)

	node1.AttempElection()

}
