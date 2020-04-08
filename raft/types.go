package raft

type appendEntriesResultWithContext struct {
	result        AppendEntriesResult
	peerID        int
	prevLogIndex  int
	entriesNumber int
}

type commandRequest struct {
	command interface{}
	result  chan CommandResult
}

type commandResultWithContext struct {
	logIndex int
	result   interface{}
}

type getStateRequest struct {
	result chan NodeState
}
