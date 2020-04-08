package raft_test

import (
	"errors"

	"github.com/konrad/raft"
)

type command struct {
	key   string
	value int
}

type stateMachine struct {
	state map[string]int
}

func (fsm stateMachine) Apply(c interface{}) interface{} {
	command := c.(command)
	value, ok := fsm.state[command.key]
	if ok {
		fsm.state[command.key] = value + command.value
	} else {
		fsm.state[command.key] = command.value
	}
	return fsm.state
}

func (fsm stateMachine) GetState() interface{} {
	return fsm.state
}

func newStateMachine() stateMachine {
	return stateMachine{
		state: make(map[string]int),
	}
}

type localTransport struct {
	address       raft.NodeAddress
	localChannels map[raft.NodeAddress]*raftChannels
	subscriptions map[raft.NodeAddress]bool
	closeCh       chan struct{}
	isOpen        bool
	isPaused      bool
}

type raftChannels struct {
	requestVoteCh   chan raft.RequestVoteRequest
	appendEntriesCh chan raft.AppendEntriesRequest
	subscription    bool
}

func (transport *localTransport) RequestVote(target raft.NodeAddress, args raft.RequestVoteArgs) (raft.RequestVoteResult, error) {
	if !transport.isPaused && transport.localChannels[target].subscription {
		result := make(chan raft.RequestVoteResult)
		defer close(result)

		request := raft.RequestVoteRequest{
			Args:   args,
			Result: result,
		}
		transport.localChannels[target].requestVoteCh <- request
		return <-result, nil
	}
	return raft.RequestVoteResult{}, errors.New("Peer not responding")
}

func (transport *localTransport) AppendEntries(target raft.NodeAddress, args *raft.AppendEntriesArgs) (raft.AppendEntriesResult, error) {
	if !transport.isPaused && transport.localChannels[target].subscription {
		result := make(chan raft.AppendEntriesResult)
		defer close(result)

		request := raft.AppendEntriesRequest{
			Args:   args,
			Result: result,
		}
		transport.localChannels[target].appendEntriesCh <- request
		return <-result, nil
	}
	return raft.AppendEntriesResult{}, errors.New("Peer not responding")
}

func (transport *localTransport) Close() {
	if transport.isOpen {
		transport.closeCh <- struct{}{}
	}
	close(transport.closeCh)
}

func (t *localTransport) Subscribe(requestVoteCh chan raft.RequestVoteRequest, appendEntriesCh chan raft.AppendEntriesRequest) {
	t.isOpen = true
	t.localChannels[t.address].subscription = true
	go func() {
	Loop:
		for {
			select {
			case requestVoteRequest := <-t.localChannels[t.address].requestVoteCh:
				if !t.isPaused {
					requestVoteCh <- requestVoteRequest
				}
			case appendEntriesRequest := <-t.localChannels[t.address].appendEntriesCh:
				if !t.isPaused {
					appendEntriesCh <- appendEntriesRequest
				}
			case <-t.closeCh:
				break Loop
			}
		}
	}()
}

func (t *localTransport) pause() {
	t.isPaused = true
}

func (t *localTransport) resume() {
	t.isPaused = false
}

func newLocalTransport(address raft.NodeAddress, localChannels map[raft.NodeAddress]*raftChannels) *localTransport {
	return &localTransport{
		address:       address,
		localChannels: localChannels,
		closeCh:       make(chan struct{}),
	}
}
