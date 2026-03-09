# Gatling Use-Case Battery — Developer & AI Agent Scenarios

Targeted queries that simulate real developer and AI agent use cases.
All symbols verified against cartograph context output (2026-04-20).

## Investigation 1: HTTP redirect handling (6 symbols)

Query keyword: `"redirect follow permanent cache max redirect naming strategy"`
Query intent: `"how does gatling handle HTTP redirects"`

Expected symbols:
- `FollowUpProcessor` — gatling-http/src/main/scala/io/gatling/http/engine/response/FollowUpProcessor.scala — builds redirect follow-up requests
- `PermanentRedirectCacheSupport` — gatling-http/src/main/scala/io/gatling/http/cache/PermanentRedirectCacheSupport.scala — caches and resolves permanent redirects
- `disableFollowRedirect` — gatling-http-java/src/main/java/io/gatling/javaapi/http/HttpProtocolBuilder.java — DSL method to disable automatic redirect following
- `maxRedirects` — gatling-http-java/src/main/java/io/gatling/javaapi/http/HttpProtocolBuilder.java — DSL method to set max redirect hops
- `redirectNamingStrategy` — gatling-http-java/src/main/java/io/gatling/javaapi/http/HttpProtocolBuilder.java — DSL method to configure redirect request naming
- `HttpTxExecutor` — gatling-http/src/main/scala/io/gatling/http/engine/tx/HttpTxExecutor.scala — executes HTTP transactions including redirect chains

## Investigation 2: Throttling implementation (6 symbols)

Query keyword: `"throttle rate limiting implementation request per second"`
Query intent: `"how does gatling implement request rate throttling"`

Expected symbols:
- `Throttler` — gatling-core/src/main/scala/io/gatling/core/controller/throttle/Throttler.scala — actor enforcing request rate limits during simulation
- `Throttle` — gatling-core/src/main/scala/io/gatling/core/controller/throttle/Throttler.scala — throttle configuration holding target RPS profile
- `ThrottlingSupport` — gatling-core/src/main/scala/io/gatling/core/controller/throttle/ThrottlingSupport.scala — DSL trait providing reachRps/holdFor/jumpToRps
- `throttle` — gatling-core-java/src/main/java/io/gatling/javaapi/core/Simulation.java — DSL method to apply throttling to a simulation
- `sendOrEnqueueRequest` — gatling-core/src/main/scala/io/gatling/core/controller/throttle/Throttler.scala — core method deciding send vs queue per rate limit
- `StartedData` — gatling-core/src/main/scala/io/gatling/core/controller/throttle/Throttler.scala — runtime state tracking current RPS and request queue

## Investigation 3: CSV/data feeding (6 symbols)

Query keyword: `"CSV feeder read file data feed strategy queue circular"`
Query intent: `"how does the CSV feeder work in gatling"`

Expected symbols:
- `FeederBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/FeederBuilder.java — configures feeders with strategy (queue, circular, random) and source
- `BatchedSeparatedValuesFeeder` — gatling-core/src/main/scala/io/gatling/core/feeder/BatchedSeparatedValuesFeeder.scala — efficient batched CSV/TSV reader
- `FeederSupport` — gatling-core/src/main/scala/io/gatling/core/feeder/FeederSupport.scala — DSL trait providing csv(), tsv(), ssv() factory methods
- `FeedActor` — gatling-core/src/main/scala/io/gatling/core/action/FeedActor.scala — actor executing feed operations during simulation
- `Feeds` — gatling-core-java/src/main/java/io/gatling/javaapi/core/feed/Feeds.java — DSL trait providing feed() on chains and scenarios
- `SeparatedValuesParser` — gatling-core/src/main/scala/io/gatling/core/feeder/SeparatedValuesParser.scala — parses CSV/TSV files into records

## Investigation 4: Simulation results and reporting (6 symbols)

Query keyword: `"write simulation results file log report output generate"`
Query intent: `"how are simulation results written to disk and reports generated"`

Expected symbols:
- `LogFileDataWriter` — gatling-core/src/main/scala/io/gatling/core/stats/writer/LogFileDataWriter.scala — writes raw stats to simulation.log during run
- `RunResultProcessor` — gatling-app/src/main/scala/io/gatling/app/RunResultProcessor.scala — post-run processor that triggers report generation
- `ReportsGenerator` — gatling-charts/src/main/scala/io/gatling/charts/report/ReportsGenerator.scala — generates HTML chart reports from simulation data
- `TemplateWriter` — gatling-charts/src/main/scala/io/gatling/charts/report/TemplateWriter.scala — writes rendered report templates to disk
- `LogFileReader` — gatling-charts/src/main/scala/io/gatling/charts/stats/LogFileReader.scala — parses simulation.log back for report generation
- `simulationLogDirectory` — gatling-core/src/main/scala/io/gatling/core/config/GatlingFiles.scala — resolves output directory for simulation logs

## Investigation 5: WebSocket support (6 symbols)

Query keyword: `"websocket connect close send frame message check"`
Query intent: `"how does gatling handle WebSocket connections"`

Expected symbols:
- `Ws` — gatling-http-java/src/main/java/io/gatling/javaapi/http/Ws.java — DSL entry point for WebSocket actions
- `WebSocket` — gatling-http-client/src/main/java/io/gatling/http/client/WebSocket.java — low-level WebSocket interface for sending frames
- `WsConnectActionBuilder` — gatling-http-java/src/main/java/io/gatling/javaapi/http/WsConnectActionBuilder.java — builds WebSocket connect actions
- `WsFsm` — gatling-http/src/main/scala/io/gatling/http/action/ws/fsm/WsFsm.scala — finite state machine managing WebSocket lifecycle
- `WsListener` — gatling-http/src/main/scala/io/gatling/http/action/ws/WsListener.scala — handles WebSocket events (open, message, close, error)
- `WsClose` — gatling-http/src/main/scala/io/gatling/http/action/ws/WsClose.scala — action that closes a WebSocket connection

## Investigation 6: Virtual user session state (6 symbols)

Query keyword: `"session set get attribute virtual user state counter"`
Query intent: `"how does the virtual user session store and manage state"`

Expected symbols:
- `Session` — gatling-core-java/src/main/java/io/gatling/javaapi/core/Session.java — core session object carrying virtual user state
- `SessionAttribute` — gatling-core/src/main/scala/io/gatling/core/session/Session.scala — typed accessor for session attributes
- `SessionPrivateAttributes` — gatling-core/src/main/scala/io/gatling/core/session/Session.scala — internal session attributes for framework use
- `set` — gatling-core-java/src/main/java/io/gatling/javaapi/core/Session.java — sets a named attribute on the session
- `remove` — gatling-core/src/main/scala/io/gatling/core/session/Session.scala — removes a named attribute from the session
- `FeedActor` — gatling-core/src/main/scala/io/gatling/core/action/FeedActor.scala — populates session attributes from data feeders
