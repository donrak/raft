package raft

// NodeAddress represents address of raft node used for communication between nodes.
type NodeAddress string

//Node represent node of a raft cluster.
type Node interface {
	GetState() NodeState
	ProcessCommand(command interface{}) CommandResult
	Start() error
	Shutdown() error
}

//CommandResult - type of object returned by a raft node when processing a command.
type CommandResult struct {
	Result interface{}
	Err    error
}

//NodeState represents the public node state together with current state machine state.
type NodeState struct {
	NodeID      int
	LeaderID    int
	CurrentTerm int
	Log         []LogEntry
	CommitIndex int
	LastApplied int
	State       interface{}
	Mode        int
}

//StateMachine is an interface for which client of a raft package should provide implementation.
//Represents state machine for raft cluster.
type StateMachine interface {
	Apply(interface{}) interface{}
	GetState() interface{}
}

//Transport is an interface for which client of a raft package should provide implementation.
//Allows communication between raft nodes.
type Transport interface {
	RequestVote(target NodeAddress, args RequestVoteArgs) (RequestVoteResult, error)
	AppendEntries(target NodeAddress, args *AppendEntriesArgs) (AppendEntriesResult, error)
	Subscribe(chan RequestVoteRequest, chan AppendEntriesRequest)
	Close()
}

//LogEntry (element of raft consensus algorithm) contains a command together with the term when it arrived to the node's log.
type LogEntry struct {
	Command interface{}
	Term    int
}

//AppendEntriesArgs (element of raft consensus algorithm) contains arguments sent by Leader to Followers for log replication.
type AppendEntriesArgs struct {
	Term         int
	LeaderID     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

//AppendEntriesResult (element of raft consensus algorithm) contains result of AppendEntries method returned by Follower to Leader.
type AppendEntriesResult struct {
	Term    int
	Success bool
}

//RequestVoteArgs (element of raft consensus algorithm) contains arguments sent by Candidate to other nodes for leader election.
type RequestVoteArgs struct {
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

//RequestVoteResult (element of raft consensus algorithm) contains result of RequestVote method returned to the Candidate.
type RequestVoteResult struct {
	Term        int
	VoteGranted bool
}

//RequestVoteRequest dd
type RequestVoteRequest struct {
	Args   RequestVoteArgs
	Result chan RequestVoteResult
}

//AppendEntriesRequest as
type AppendEntriesRequest struct {
	Args   *AppendEntriesArgs
	Result chan AppendEntriesResult
}
