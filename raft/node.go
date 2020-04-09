package raft

import (
	"errors"
	"sync"
	"time"
)

type clusterNode struct {
	id                  int
	peers               map[int]NodeAddress
	state               *state
	controller          *nodeController
	stateMachine        StateMachine	
	shutdownLock        sync.RWMutex
	commandResultsChMap map[int]chan CommandResult
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (node *clusterNode) switchToFollowerMode(term int, leaderID int) {
	node.state.currentTerm = term
	node.state.votedFor = -1
	node.state.leaderID = leaderID
	if node.state.getMode() == Leader {
		node.controller.stopHeartbeat()
		node.controller.runElectionTimer()
	}
	node.state.setMode(Follower)
}

func (node *clusterNode) switchToCandidateMode() {
	node.state.currentTerm++
	node.state.votedFor = node.id
	node.state.votesReceived = 1
	node.state.setMode(Candidate)
}

func (node *clusterNode) switchToLeaderMode() {
	node.controller.stopElectionTimer()
	node.state.setMode(Leader)
	node.state.leaderID = node.id
	node.controller.runHeartbeat()
}

func (node *clusterNode) getQuorum() int {
	return len(node.peers)/2 + 1
}

func (node *clusterNode) requestVote(args RequestVoteArgs) RequestVoteResult {
	result := RequestVoteResult{}
	state := node.state

	if args.Term > state.currentTerm {
		node.switchToFollowerMode(args.Term, -1)
	}

	lastLogIndex, lastLogTerm := state.lastLogIndexAndTerm()

	if args.Term == state.currentTerm &&
		state.votedFor == -1 &&
		(args.LastLogTerm > lastLogTerm ||
			(args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)) {
		result.VoteGranted = true
		state.votedFor = args.CandidateID
		node.controller.resetElectionTimeout()
	}

	result.Term = state.currentTerm

	return result
}

func (node *clusterNode) appendEntries(args *AppendEntriesArgs) AppendEntriesResult {
	state := node.state
	result := AppendEntriesResult{
		Term: state.currentTerm,
	}

	if args.Term < state.currentTerm {
		return result
	}

	node.controller.resetElectionTimeout()

	if args.Term > state.currentTerm || state.leaderID == -1 {
		node.switchToFollowerMode(args.Term, args.LeaderID)
	}

	lastLogIndex, _ := state.lastLogIndexAndTerm()
	if args.PrevLogIndex == -1 ||
		(args.PrevLogIndex <= lastLogIndex && state.log[args.PrevLogIndex].Term == args.PrevLogTerm) {
		result.Success = true

		state.log = appendEntries(state.log, args.Entries, args.PrevLogIndex)
		if args.LeaderCommit > node.state.commitIndex {
			node.state.commitIndex = min(args.LeaderCommit, len(state.log)-1)
			node.controller.applyEntries()
		}
	}

	return result
}

func appendEntries(log []LogEntry, newEntries []LogEntry, prevLogIndex int) []LogEntry {
	logInsertIndex := prevLogIndex + 1
	newEntriesIndex := 0

	for {
		if logInsertIndex >= len(log) || newEntriesIndex >= len(newEntries) {
			break
		}
		if log[logInsertIndex].Term != newEntries[newEntriesIndex].Term {
			break
		}
		logInsertIndex++
		newEntriesIndex++
	}

	return append(log[:logInsertIndex], newEntries[newEntriesIndex:]...)
}

func (node *clusterNode) startElection() {
	node.switchToCandidateMode()
	if len(node.peers) == 0 {
		node.switchToLeaderMode()
		return
	}

	lastLogIndex, lastLogTerm := node.state.lastLogIndexAndTerm()

	args := RequestVoteArgs{
		Term:         node.state.currentTerm,
		CandidateID:  node.id,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	for _, peerAddress := range node.peers {
		node.controller.sendRequestVote(peerAddress, args)
	}
}

func (node *clusterNode) processVoteResult(result RequestVoteResult) {
	if node.state.getMode() != Candidate {
		return
	}

	if result.Term > node.state.currentTerm {
		node.switchToFollowerMode(result.Term, -1)
	}

	if result.VoteGranted {
		if node.state.votesReceived++; node.state.votesReceived >= node.getQuorum() {
			node.switchToLeaderMode()
		}
	}
}

func (node *clusterNode) processAppendEntriesResult(resultWithContext appendEntriesResultWithContext) {
	if node.state.getMode() != Leader {
		return
	}

	result := resultWithContext.result
	if result.Term > node.state.currentTerm {
		node.switchToFollowerMode(result.Term, -1)
		return
	}

	if result.Success {
		matchIndex := resultWithContext.prevLogIndex + resultWithContext.entriesNumber
		node.state.matchIndex[resultWithContext.peerID] = matchIndex
		node.state.nextIndex[resultWithContext.peerID] = matchIndex + 1

		commitIndex := node.state.commitIndex
		if commitIndex < matchIndex {
			for i := matchIndex; i > commitIndex; i-- {
				matchCount := 1
				for peerID := range node.peers {
					if node.state.matchIndex[peerID] >= i {
						matchCount++
					}
				}
				if matchCount >= node.getQuorum() {
					node.state.commitIndex = i
					node.controller.applyEntries()
					node.controller.broadcastAppendEntries()
					break
				}
			}
		}
	} else if node.state.nextIndex[resultWithContext.peerID] > 0 {
		node.state.nextIndex[resultWithContext.peerID]--
	}
}

func (node *clusterNode) sendAppendEntries() {
	state := node.state
	node.controller.resetHeartbeatTimeout()

	if len(node.peers) == 0 {
		state.commitIndex = len(state.log) - 1
		node.controller.applyEntries()
	}

	for peerID, peerAddress := range node.peers {
		prevLogIndex := state.nextIndex[peerID] - 1
		prevLogTerm := -1
		entries := state.log
		if prevLogIndex != -1 {
			prevLogTerm = state.log[prevLogIndex].Term
			entries = state.log[state.nextIndex[peerID]:]
		}

		args := &AppendEntriesArgs{
			Term:         state.currentTerm,
			LeaderID:     node.id,
			PrevLogIndex: prevLogIndex,
			PrevLogTerm:  prevLogTerm,
			Entries:      entries,
			LeaderCommit: state.commitIndex,
		}
		node.controller.sendAppendEntries(peerAddress, args, peerID, prevLogIndex)
	}
}

func (node *clusterNode) processCommandRequest(commandRequest commandRequest) {
	if node.state.getMode() == Leader {
		newLogEntry := LogEntry{
			Command: commandRequest.command,
			Term:    node.state.currentTerm,
		}
		node.state.log = append(node.state.log, newLogEntry)
		logIndex := len(node.state.log) - 1
		node.commandResultsChMap[logIndex] = commandRequest.result
		node.controller.broadcastAppendEntries()
	} else {
		commandRequest.result <- CommandResult{
			Err: errors.New("Node is not a leader"),
		}
	}
}

func (node *clusterNode) processCommandResult(commandResultWithContext commandResultWithContext) {
	commandResultCh, ok := node.commandResultsChMap[commandResultWithContext.logIndex]
	if ok {
		commandResultCh <- CommandResult{
			Result: commandResultWithContext.result,
			Err:    nil,
		}
		delete(node.commandResultsChMap, commandResultWithContext.logIndex)
	}
}

func (node *clusterNode) applyEntries() {
	for node.state.lastApplied < node.state.commitIndex {
		logIndexToApply := node.state.lastApplied + 1
		commandResult := node.stateMachine.Apply(node.state.log[logIndexToApply].Command)
		node.state.lastApplied++
		node.controller.acknowledgeCommandResult(commandResultWithContext{
			logIndex: logIndexToApply,
			result:   commandResult,
		})
	}
}

func (node *clusterNode) getState(request getStateRequest) {
	result := NodeState{
		NodeID:      node.id,
		LeaderID:    node.state.leaderID,
		CurrentTerm: node.state.currentTerm,
		CommitIndex: node.state.commitIndex,
		LastApplied: node.state.lastApplied,
		State:       node.stateMachine.GetState(),
		Log:         node.state.log,
		Mode:        int(node.state.mode),
	}

	request.result <- result
}

func (node *clusterNode) shutdown() {
	if node.state.getMode() == Leader {
		node.controller.stopHeartbeat()
	} else {
		node.controller.stopElectionTimer()
	}
	node.state.setMode(Shutdown)
	node.state.leaderID = -1
	node.shutdownLock.Unlock()
}

//NewNode is a method constructing raft cluster node which implement raft.Node interface
func NewNode(id int, peers map[int]NodeAddress, transport Transport, stateMachine StateMachine) Node {
	state := &state{
		commitIndex:   -1,
		currentTerm:   0,
		lastApplied:   -1,
		leaderID:      -1,
		log:           make([]LogEntry, 0),
		matchIndex:    make(map[int]int),
		nextIndex:     make(map[int]int),
		mode:          NotStarted,
		votedFor:      -1,
		votesReceived: 0,
	}

	for peerID := range peers {
		state.matchIndex[peerID] = -1
		state.nextIndex[peerID] = 0
	}

	node := &clusterNode{
		id:                  id,
		peers:               peers,
		state:               state,
		stateMachine:        stateMachine,		
		commandResultsChMap: make(map[int]chan CommandResult),
	}

	node.controller = newNodeController(node, transport)

	return node
}

func (node *clusterNode) GetState() NodeState {
	node.shutdownLock.RLock()
	defer node.shutdownLock.RUnlock()

	getStateResultCh := make(chan NodeState)
	defer close(getStateResultCh)

	request := getStateRequest{
		result: getStateResultCh,
	}

	mode := node.state.getMode()
	if mode == NotStarted || mode == Shutdown {
		go node.getState(request)
	} else {
		node.controller.processGetStateRequest(request)
	}

	return <-getStateResultCh
}

func (node *clusterNode) ProcessCommand(command interface{}) CommandResult {
	node.shutdownLock.RLock()
	defer node.shutdownLock.RUnlock()

	if node.state.getMode() == Leader {
		commandResultCh := make(chan CommandResult)
		defer close(commandResultCh)

		node.controller.processCommand(commandRequest{
			command: command,
			result:  commandResultCh,
		})

		select {
		case commandResult := <-commandResultCh:
			return commandResult
		case <-time.After(time.Millisecond * 100):
			return CommandResult{
				Err: errors.New("Timeout error"),
			}
		}
	}
	return CommandResult{
		Err: errors.New("Command can be processed only on Leader node"),
	}
}

func (node *clusterNode) Shutdown() error {
	node.shutdownLock.Lock()

	if node.state.getMode() != Shutdown {
		node.controller.shutdown()
		return nil
	}
	return errors.New("The node has already been shutdown")
}

func (node *clusterNode) Start() error {
	if node.state.getMode() == NotStarted {
		node.state.setMode(Follower)
		node.controller.run()
		node.controller.runElectionTimer()
		return nil
	}
	return errors.New("Can't start node which already has been started")
}
