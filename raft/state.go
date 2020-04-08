package raft

import "sync/atomic"

type nodeMode uint32

const (
	// NotStarted is the initial mode of a node.
	NotStarted nodeMode = iota

	// Follower is one of the valid modes of a running node.
	Follower

	// Candidate is one of the valid modes of a running node.
	Candidate

	// Leader is one of the valid modes of a running node.
	Leader

	// Shutdown is the final mode when node stopped working
	Shutdown
)

type state struct {
	currentTerm   int
	votedFor      int
	votesReceived int
	log           []LogEntry
	commitIndex   int
	lastApplied   int
	nextIndex     map[int]int
	matchIndex    map[int]int
	mode          nodeMode
	leaderID      int
}

func (state *state) lastLogIndexAndTerm() (int, int) {
	if len(state.log) > 0 {
		lastIndex := len(state.log) - 1
		return lastIndex, state.log[lastIndex].Term
	}
	return -1, -1
}

func (state *state) getMode() nodeMode {
	modeAddr := (*uint32)(&state.mode)
	return nodeMode(atomic.LoadUint32(modeAddr))
}

func (state *state) setMode(mode nodeMode) {
	modeAddr := (*uint32)(&state.mode)
	atomic.StoreUint32(modeAddr, uint32(mode))
}
