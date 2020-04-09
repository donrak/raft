package raft

import (
	"math/rand"
	"time"
)

type nodeController struct {
	requestVoteResultCh     chan RequestVoteResult
	requestVoteRequestCh    chan RequestVoteRequest
	appendEntriesResultCh   chan appendEntriesResultWithContext
	appendEntriesRequestCh  chan AppendEntriesRequest
	applyEntriesCh          chan struct{}
	startElectionCh         chan struct{}
	resetElectionTimeoutCh  chan struct{}
	stopElectionTimerCh     chan struct{}
	resetHeartbeatTimeoutCh chan struct{}
	stopHeartbeatCh         chan struct{}
	sendAppendEntriesCh     chan struct{}
	shutdownCh              chan struct{}
	requestCommandRequestCh chan commandRequest
	commandResultsCh        chan commandResultWithContext
	getStateRequestCh       chan getStateRequest
	node                    *clusterNode
	transport               Transport
}

func newNodeController(node *clusterNode, transport Transport) *nodeController {
	return &nodeController{
		requestVoteResultCh:     make(chan RequestVoteResult),
		requestVoteRequestCh:    make(chan RequestVoteRequest),
		appendEntriesResultCh:   make(chan appendEntriesResultWithContext),
		appendEntriesRequestCh:  make(chan AppendEntriesRequest),
		applyEntriesCh:          make(chan struct{}),
		startElectionCh:         make(chan struct{}),
		resetElectionTimeoutCh:  make(chan struct{}),
		stopElectionTimerCh:     make(chan struct{}),
		resetHeartbeatTimeoutCh: make(chan struct{}),
		stopHeartbeatCh:         make(chan struct{}),
		sendAppendEntriesCh:     make(chan struct{}),
		shutdownCh:              make(chan struct{}),
		requestCommandRequestCh: make(chan commandRequest),
		getStateRequestCh:       make(chan getStateRequest),
		commandResultsCh:        make(chan commandResultWithContext),
		node:                    node,
		transport:               transport,
	}
}

func (c *nodeController) run() {
	go func() {
		c.transport.Subscribe(c.requestVoteRequestCh, c.appendEntriesRequestCh)
		for {
			select {
			case requestVoteRequest := <-c.requestVoteRequestCh:
				requestVoteRequest.Result <- c.node.requestVote(requestVoteRequest.Args)
			case requestVoteResult := <-c.requestVoteResultCh:
				c.node.processVoteResult(requestVoteResult)
			case appendEntriesResultWithContext := <-c.appendEntriesResultCh:
				c.node.processAppendEntriesResult(appendEntriesResultWithContext)
			case appendEntriesRequest := <-c.appendEntriesRequestCh:
				appendEntriesRequest.Result <- c.node.appendEntries(appendEntriesRequest.Args)
			case <-c.sendAppendEntriesCh:
				c.node.sendAppendEntries()
			case <-c.applyEntriesCh:
				c.node.applyEntries()
			case <-c.startElectionCh:
				c.node.startElection()
			case commandRequest := <-c.requestCommandRequestCh:
				c.node.processCommandRequest(commandRequest)
			case commandResultWithContext := <-c.commandResultsCh:
				c.node.processCommandResult(commandResultWithContext)
			case getStateRequest := <-c.getStateRequestCh:
				c.node.getState(getStateRequest)
			case <-c.shutdownCh:
				{
					c.transport.Close()
					c.node.shutdown()
					break
				}
			}
		}
	}()
}

func (c *nodeController) runElectionTimer() {
	go func() {
		electionTimeout := time.Duration(150+rand.Intn(150)) * time.Millisecond
	ElectionTimeoutLoop:
		for {
			electionTimer := time.NewTimer(electionTimeout)
			select {
			case <-electionTimer.C:
				c.startElectionCh <- struct{}{}
			case <-c.resetElectionTimeoutCh:
				break
			case <-c.stopElectionTimerCh:
				electionTimer.Stop()
				break ElectionTimeoutLoop
			}
			electionTimer.Stop()
		}
	}()
}

func (c *nodeController) resetElectionTimeout() {
	go func() {
		c.resetElectionTimeoutCh <- struct{}{}
	}()
}

func (c *nodeController) stopElectionTimer() {
	go func() {
		c.stopElectionTimerCh <- struct{}{}
	}()
}

func (c *nodeController) runHeartbeat() {
	go func() {
		c.sendAppendEntriesCh <- struct{}{}
		heartbeatTimeout := time.Duration(50) * time.Millisecond
	HeartbeatLoop:
		for {
			heartbeatTimer := time.NewTimer(heartbeatTimeout)
			select {
			case <-heartbeatTimer.C:
				c.sendAppendEntriesCh <- struct{}{}
			case <-c.resetHeartbeatTimeoutCh:
				break
			case <-c.stopHeartbeatCh:
				heartbeatTimer.Stop()
				break HeartbeatLoop
			}
			heartbeatTimer.Stop()
		}
	}()
}

func (c *nodeController) resetHeartbeatTimeout() {
	go func() {
		c.resetHeartbeatTimeoutCh <- struct{}{}
	}()
}

func (c *nodeController) stopHeartbeat() {
	go func() {
		c.stopHeartbeatCh <- struct{}{}
	}()
}

func (c *nodeController) sendRequestVote(address NodeAddress, args RequestVoteArgs) {
	go func() {
		voteResult, err := c.transport.RequestVote(address, args)
		if err == nil {
			c.requestVoteResultCh <- voteResult
		}
	}()
}

func (c *nodeController) applyEntries() {
	go func() {
		c.applyEntriesCh <- struct{}{}
	}()
}

func (c *nodeController) broadcastAppendEntries() {
	go func() {
		c.sendAppendEntriesCh <- struct{}{}
	}()
}

func (c *nodeController) sendAppendEntries(address NodeAddress, args *AppendEntriesArgs, peerID int, prevLogIndex int) {
	go func() {
		result, err := c.transport.AppendEntries(address, args)

		if err == nil {
			resultWithContext := appendEntriesResultWithContext{
				result:        result,
				peerID:        peerID,
				prevLogIndex:  prevLogIndex,
				entriesNumber: len(args.Entries),
			}
			c.appendEntriesResultCh <- resultWithContext
		}
	}()
}

func (c *nodeController) processCommand(commandRequest commandRequest) {
	go func() {
		c.requestCommandRequestCh <- commandRequest
	}()
}

func (c *nodeController) acknowledgeCommandResult(commandResultWithContext commandResultWithContext) {
	go func() {
		c.commandResultsCh <- commandResultWithContext
	}()
}

func (c *nodeController) processGetStateRequest(request getStateRequest) {
	go func() {
		c.getStateRequestCh <- request
	}()
}

func (c *nodeController) shutdown() {
	c.shutdownCh <- struct{}{}
}