package raft_test

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/donrak/raft"
)

func TestSingleNode(t *testing.T) {
	stateMachine := newStateMachine()
	localChannels := make(map[raft.NodeAddress]*raftChannels)
	localChannels["node1"] = &raftChannels{
		appendEntriesCh: make(chan raft.AppendEntriesRequest),
		requestVoteCh:   make(chan raft.RequestVoteRequest),
		subscription:    false,
	}
	transport := newLocalTransport("node1", localChannels)
	peers := make(map[int]raft.NodeAddress)
	node := raft.NewNode(1, peers, transport, stateMachine)

	node.Start()
	state := node.GetState()
	for state.LeaderID != 1 {
		<-time.After(time.Millisecond * 10)
		state = node.GetState()
	}

	processCommands(node, "X", 5)

	node.Shutdown()
	state = node.GetState()
	if state.State.(map[string]int)["X"] != 10 {
		t.Errorf("State should have different value. Currrent state: %#v", state)
	}
}

func TestMultipleNodes(t *testing.T) {
	nodes, _ := initNodes()
	nodes[1].Start()
	nodes[2].Start()
	nodes[3].Start()

	state := nodes[1].GetState()
	for state.LeaderID == -1 {
		<-time.After(time.Millisecond)
		state = nodes[1].GetState()
	}

	leaderNode := nodes[state.LeaderID]

	processCommands(leaderNode, "X", 100)

	state1 := nodes[1].GetState().State.(map[string]int)["X"]
	state2 := nodes[2].GetState().State.(map[string]int)["X"]
	state3 := nodes[3].GetState().State.(map[string]int)["X"]

	if state1 != 4950 || state2 != 4950 || state3 != 4950 {
		t.Errorf("State should have different value.")
	}
}

func TestProcessCommandByFollower(t *testing.T) {
	nodes, _ := initNodes()
	nodes[1].Start()
	nodes[2].Start()
	nodes[3].Start()

	state := nodes[1].GetState()
	for state.LeaderID == -1 {
		<-time.After(time.Millisecond)
		state = nodes[1].GetState()
	}
	followerId := 1

	if state.LeaderID == 1 {
		followerId = 2
	}

	followerNode := nodes[followerId]

	result := followerNode.ProcessCommand(command{key: "X", value: 1})
	if result.Err == nil {
		t.Errorf("Followers shouldn't be able to process commands.")
	}

	state1 := nodes[1].GetState().State.(map[string]int)
	state2 := nodes[2].GetState().State.(map[string]int)
	state3 := nodes[3].GetState().State.(map[string]int)

	if len(state1) != 0 || len(state2) != 0 || len(state3) != 0 {
		t.Errorf("State shouldn't have any values.")
	}
}

func TestFailedSingleFollower(t *testing.T) {

	nodes, transports := initNodes()
	nodes[1].Start()
	nodes[2].Start()
	nodes[3].Start()

	state := nodes[1].GetState()
	for state.LeaderID == -1 {
		<-time.After(time.Millisecond)
		state = nodes[1].GetState()
	}
	leaderNode := nodes[state.LeaderID]

	failedfollowerId := 1
	if state.LeaderID == 1 {
		failedfollowerId = 2
	}
	transports[failedfollowerId].pause()

	leaderNode.ProcessCommand(command{key: "X", value: 1})

	leaderState := leaderNode.GetState().State.(map[string]int)
	if leaderState["X"] != 1 {
		t.Errorf("State should have correct value. Current state: %#v", leaderState)
	}

	failedfollowerState := nodes[failedfollowerId].GetState().State.(map[string]int)
	if len(failedfollowerState) > 0 {
		t.Errorf("Failed follower should not have any state. Current state: %#v", failedfollowerState)
	}

	transports[failedfollowerId].resume()
	<-time.After(time.Millisecond * 100)

	failedfollowerState = nodes[failedfollowerId].GetState().State.(map[string]int)
	if failedfollowerState["X"] != 1 {
		t.Errorf("Follower should have the same state as leader. Current state: %#v", failedfollowerState)
	}
}

func TestFailedAllFollowers(t *testing.T) {

	nodes, transports := initNodes()
	nodes[1].Start()
	nodes[2].Start()
	nodes[3].Start()

	state := nodes[1].GetState()
	for state.LeaderID == -1 {
		<-time.After(time.Millisecond)
		state = nodes[1].GetState()
	}
	leaderNode := nodes[state.LeaderID]
	for i := 1; i < 4; i++ {
		if i != state.LeaderID {
			transports[i].pause()
		}
	}

	result := leaderNode.ProcessCommand(command{key: "X", value: 1})
	if result.Err == nil {
		t.Errorf("Leader without quorum shouldn't be able to process commands.")
	}
}

func TestFailedLeader(t *testing.T) {

	nodes, transports := initNodes()
	nodes[1].Start()
	nodes[2].Start()
	nodes[3].Start()

	state := nodes[1].GetState()
	for state.LeaderID == -1 {
		<-time.After(time.Millisecond)
		state = nodes[1].GetState()
	}
	originalLeaderId := state.LeaderID	
	processCommands(nodes[originalLeaderId], "X", 4)
	transports[originalLeaderId].pause()

	followerId := 1
	if state.LeaderID == 1 {
		followerId = 2
	}

	state = nodes[followerId].GetState()
	for state.LeaderID == originalLeaderId {
		<-time.After(time.Millisecond)
		state = nodes[followerId].GetState()
	}

	newLeaderId := state.LeaderID	
	processCommands(nodes[newLeaderId], "Y", 4)
	transports[originalLeaderId].resume()

	<-time.After(time.Millisecond * 100)
	originalLeaderState := nodes[originalLeaderId].GetState().State.(map[string]int)
	if originalLeaderState["Y"] != 6 {
		t.Errorf("State should have correct value. Current state: %#v", originalLeaderState)
	}
}

func initNodes() (map[int]raft.Node, map[int]*localTransport) {
	localChannels := make(map[raft.NodeAddress]*raftChannels)
	cluster := make(map[int]raft.NodeAddress)
	nodes := make(map[int]raft.Node)
	transports := make(map[int]*localTransport)

	for i := 1; i < 4; i++ {
		cluster[i] = raft.NodeAddress("node" + strconv.Itoa(i))
		localChannels[cluster[i]] = &raftChannels{
			appendEntriesCh: make(chan raft.AppendEntriesRequest),
			requestVoteCh:   make(chan raft.RequestVoteRequest),
			subscription:    false,
		}
	}

	for i := 1; i < 4; i++ {
		transports[i] = newLocalTransport(cluster[i], localChannels)
		peers := make(map[int]raft.NodeAddress)
		for k := 1; k < 4; k++ {
			if k != i {
				peers[k] = cluster[k]
			}
		}
		nodes[i] = raft.NewNode(i, peers, transports[i], newStateMachine())
	}
	return nodes, transports
}

func processCommands(node raft.Node, key string, commandsNumber int) {
	var wg sync.WaitGroup
	wg.Add(commandsNumber)

	for i := 0; i < commandsNumber; i++ {
		go func(i int) {
			node.ProcessCommand(command{key: key, value: i})
			wg.Done()
		}(i)
	}

	wg.Wait()
}
