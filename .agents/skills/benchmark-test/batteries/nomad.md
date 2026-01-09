# Nomad Query Battery — Grounded Expected Symbols

All symbols verified against hashicorp/nomad graph (2026-04-05, 37587 nodes).

**Note:** Nomad is a large codebase (37K nodes, 12.5K embedded). Use `-l 15`
instead of the default `-l 8` to compensate for signal dilution.

## Investigation 1: Server startup (9 symbols)

Query keyword: `"server setup startup bootstrap"`
Query intent: `"how does nomad start"`

Expected symbols:
- `NewServer` — nomad/server.go — central coordination point for server startup
- `setupServer` — command/agent/command.go — agent-level server setup
- `NewAgent` — command/agent/agent.go — creates agent wrapping server
- `setupRPC` — nomad/server.go — configures RPC listeners
- `setupRaft` — nomad/server.go — initializes raft consensus
- `setupWorkers` — nomad/server.go — starts scheduler workers
- `monitorLeadership` — nomad/leader.go — watches raft leadership transitions
- `Start` — nomad/server.go — starts the server
- `startRPCListener` — nomad/rpc.go — starts the RPC listener

## Investigation 2: Job scheduling (8 symbols)

Query keyword: `"scheduler evaluation worker plan"`
Query intent: `"how does job scheduling work"`

Expected symbols:
- `Evaluation` — nomad/structs/structs.go — evaluation struct (scheduling trigger)
- `dequeueEvaluation` — nomad/worker.go — dequeues eval from broker
- `GenericScheduler` — scheduler/generic_sched.go — main scheduler implementation
- `Worker` — nomad/worker.go — scheduler worker struct
- `SubmitPlan` — nomad/plan_apply.go — submits plan to leader
- `BinPackIterator` — scheduler/rank.go — bin-packing placement iterator
- `EvalBroker` — nomad/eval_broker.go — evaluation broker
- `Process` — scheduler/generic_sched.go — scheduler Process method

## Investigation 3: Node failure (7 symbols)

Query keyword: `"node failed heartbeat drain"`
Query intent: `"what happens when a node fails"`

Expected symbols:
- `nodeFailed` — nomad/leader.go — handles failed node event
- `reconcileMember` — nomad/leader.go — reconciles serf member state
- `NodeDrain` — nomad/structs/structs.go — node drain configuration
- `Heartbeat` — nomad/node_endpoint.go — heartbeat RPC handler
- `getOrCreateNode` — nomad/state/state_store.go — gets or creates node record
- `handleDeregistration` — nomad/node_endpoint.go — handles node deregistration
- `Node` — nomad/structs/structs.go — node struct

## Investigation 4: Raft consensus (9 symbols)

Query keyword: `"raft leader election apply"`
Query intent: `"raft consensus implementation"`

Expected symbols:
- `monitorLeadership` — nomad/leader.go — monitors raft leadership changes
- `establishLeadership` — nomad/leader.go — runs when becoming leader
- `leaderLoop` — nomad/leader.go — main leader event loop
- `IsLeader` — nomad/server.go — checks if this server is raft leader
- `setupRaft` — nomad/server.go — initializes raft subsystem
- `RaftStats` — nomad/server.go — returns raft statistics
- `raftApply` — nomad/server.go — applies operation to raft log
- `Apply` — nomad/fsm.go — raft FSM apply method
- `Leader` — nomad/server.go — returns current leader address

## Investigation 5: Client-server communication (8 symbols)

Query keyword: `"client server RPC allocSync heartbeat"`
Query intent: `"how does the client communicate with the server"`

Expected symbols:
- `Client` — client/client.go — client struct
- `Server` — nomad/server.go — server struct
- `RPC` — nomad/server.go — RPC method handler
- `allocSync` — client/alloc_watcher.go — syncs allocation state
- `Heartbeat` — nomad/node_endpoint.go — heartbeat RPC
- `watchAllocations` — client/client.go — watches for allocation updates
- `NodeRegister` — nomad/node_endpoint.go — node registration RPC
- `getServer` — client/client.go — gets current server address
