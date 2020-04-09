# Raft

I wanted to create simple implementation of the [Raft distributed consensus algorithm](https://raft.github.io/raft.pdf) in Go.
Partially inspired by implementation by Hashicorp (https://github.com/hashicorp/raft)

One of the goal was to implement the algorithm without locks and just rely on channels communication.

To increase code readability there are some simplifications:
- all nodes must be known upfront; it's not possible to add/remove nodes dynamically
- there is no implementation of log storage and snapshots; all log entries are just kept in memory

## How to use the package

The implementation of the state machine and communication between nodes were abstracted from the algorithm.
So to start using the package it's necessary to provide types which will implement interfeaces:
```
type StateMachine interface {
	Apply(interface{}) interface{}
	GetState() interface{}
}

type Transport interface {
	RequestVote(target NodeAddress, args RequestVoteArgs) (RequestVoteResult, error)
	AppendEntries(target NodeAddress, args *AppendEntriesArgs) (AppendEntriesResult, error)
	Subscribe(chan RequestVoteRequest, chan AppendEntriesRequest)
	Close()
}
```

The interfaces together with argument types can be found in `api.go` file.
Once there are available objects implementing interfaces above it's possible to create a Node object using function:
```
func NewNode(id int, peers map[int]NodeAddress, transport Transport, stateMachine StateMachine) Node
```

The Node interface allows to retrieve the state and process the commands.
```
type Node interface {
	GetState() NodeState
	ProcessCommand(command interface{}) CommandResult
	Start() error
	Shutdown() error
}
```

The commands can be only processed by leader node which will be elected once majority of the nodes are started. 
To establish leader node you can use `GetState()` method.




