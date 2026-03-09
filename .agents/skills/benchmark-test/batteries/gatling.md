# Gatling Query Battery — Grounded Expected Symbols

All symbols verified against gatling/gatling source and cartograph context output (2026-04-20).
Expected symbols are discoverable entry points a developer exploring gatling would actually want to find.

## Investigation 1: Simulation execution (8 symbols)

Query keyword: `"simulation runner scenario setup injection controller configuration"`
Query intent: `"how does gatling run a load test simulation"`

Expected symbols:
- `Simulation` — gatling-core-java/src/main/java/io/gatling/javaapi/core/Simulation.java — core entry point: base class users extend to define load tests
- `Runner` — gatling-app/src/main/scala/io/gatling/app/Runner.scala — orchestrates full simulation lifecycle: load, setup, inject, run
- `SetUp` — gatling-core/src/main/scala/io/gatling/core/scenario/Simulation.scala — configuration object tying scenarios, protocols, and assertions together
- `Controller` — gatling-core/src/main/scala/io/gatling/core/controller/Controller.scala — central actor coordinating injection and throttling during execution
- `Injector` — gatling-core/src/main/scala/io/gatling/core/controller/inject/Injector.scala — manages user injection into scenarios at configured rates
- `SimulationParams` — gatling-core/src/main/scala/io/gatling/core/scenario/Simulation.scala — parameters extracted from simulation setUp method
- `ScenarioBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/ScenarioBuilder.java — DSL builder for constructing scenario definitions
- `GatlingConfiguration` — gatling-core/src/main/scala/io/gatling/core/config/GatlingConfiguration.scala — loads and holds all configuration settings

## Investigation 2: HTTP protocol (8 symbols)

Query keyword: `"http request response protocol engine listener client check"`
Query intent: `"how does the HTTP protocol handle requests and responses"`

Expected symbols:
- `HttpProtocolBuilder` — gatling-http-java/src/main/java/io/gatling/javaapi/http/HttpProtocolBuilder.java — DSL builder for HTTP protocol configuration
- `HttpEngine` — gatling-http/src/main/scala/io/gatling/http/engine/HttpEngine.scala — manages HTTP client connections, sends requests
- `GatlingHttpListener` — gatling-http/src/main/scala/io/gatling/http/engine/GatlingHttpListener.scala — Netty async listener handling HTTP responses
- `HttpListener` — gatling-http-client/src/main/java/io/gatling/http/client/HttpListener.java — listener interface for async HTTP response callbacks
- `HttpRequestActionBuilder` — gatling-http-java/src/main/java/io/gatling/javaapi/http/HttpRequestActionBuilder.java — DSL builder for HTTP request actions
- `HttpRequest` — gatling-http/src/main/scala/io/gatling/http/request/HttpRequest.scala — request model object built from DSL definitions
- `CheckProcessor` — gatling-http/src/main/scala/io/gatling/http/engine/response/CheckProcessor.scala — applies validation checks to HTTP responses
- `ResponseProcessor` — gatling-http/src/main/scala/io/gatling/http/engine/response/ResponseProcessor.scala — processes and validates HTTP responses

## Investigation 3: Session and statistics (8 symbols)

Query keyword: `"session stats data writer console log statistics engine"`
Query intent: `"how are session state and performance statistics tracked and written"`

Expected symbols:
- `Session` — gatling-core-java/src/main/java/io/gatling/javaapi/core/Session.java — core virtual user state object, carries attributes through scenario
- `StatsEngine` — gatling-core/src/main/scala/io/gatling/core/stats/StatsEngine.scala — central interface for recording performance statistics
- `DataWritersStatsEngine` — gatling-core/src/main/scala/io/gatling/core/stats/DataWritersStatsEngine.scala — default stats engine dispatching to multiple data writers
- `DataWriter` — gatling-core/src/main/scala/io/gatling/core/stats/writer/DataWriter.scala — base class for all statistics output writers
- `ConsoleDataWriter` — gatling-core/src/main/scala/io/gatling/core/stats/writer/ConsoleDataWriter.scala — renders live stats to console during simulation
- `LogFileDataWriter` — gatling-core/src/main/scala/io/gatling/core/stats/writer/LogFileDataWriter.scala — writes stats to simulation.log for post-run analysis
- `SessionProcessor` — gatling-http/src/main/scala/io/gatling/http/engine/response/SessionProcessor.scala — updates session state after HTTP responses
- `StatsProcessor` — gatling-http/src/main/scala/io/gatling/http/engine/response/StatsProcessor.scala — reports HTTP request/response stats to stats engine

## Investigation 4: Action chain and data feeding (8 symbols)

Query keyword: `"action chain builder exec loop pause feeder structure"`
Query intent: `"how are action chains and data feeders built in the scenario DSL"`

Expected symbols:
- `ChainBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/ChainBuilder.java — builds chains of actions composing a scenario
- `ScenarioBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/ScenarioBuilder.java — DSL builder for constructing scenarios from chains
- `Execs` — gatling-core-java/src/main/java/io/gatling/javaapi/core/exec/Execs.java — DSL mixin providing exec() operations on chains
- `FeedActor` — gatling-core/src/main/scala/io/gatling/core/action/FeedActor.scala — actor that executes data feed operations during simulation
- `Feeds` — gatling-core-java/src/main/java/io/gatling/javaapi/core/feed/Feeds.java — DSL mixin providing feed() operations on chains
- `PauseBuilder` — gatling-core/src/main/scala/io/gatling/core/action/builder/PauseBuilder.scala — builds pause actions for think time simulation
- `LoopBuilder` — gatling-core/src/main/scala/io/gatling/core/action/builder/LoopBuilder.scala — builds loop/repeat actions for iteration patterns
- `CoreDsl` — gatling-core-java/src/main/java/io/gatling/javaapi/core/CoreDsl.java — top-level DSL trait providing scenario(), exec(), feed(), pause() factories

## Investigation 5: Assertions and injection models (8 symbols)

Query keyword: `"assertion throttle injection ramp rate open closed step"`
Query intent: `"how do assertions and injection profiles control load testing"`

Expected symbols:
- `InjectionProfile` — gatling-core/src/main/scala/io/gatling/core/controller/inject/InjectionProfile.scala — interface for open/closed injection models
- `Injector` — gatling-core/src/main/scala/io/gatling/core/controller/inject/Injector.scala — executes injection profiles, manages user injection lifecycle
- `Assertion` — gatling-core-java/src/main/java/io/gatling/javaapi/core/Assertion.java — assertion model with percentile, mean, and custom condition checks
- `AssertionWithPath` — gatling-core/src/main/scala/io/gatling/core/assertion/AssertionBuilders.scala — assertion builder DSL after path selection (global/details/forAll)
- `Throttler` — gatling-core/src/main/scala/io/gatling/core/controller/throttle/Throttler.scala — actor enforcing request rate limits during simulation
- `SetUp` — gatling-core/src/main/scala/io/gatling/core/scenario/Simulation.scala — ties injection profiles, assertions, and throttling to simulation
- `RampOpenInjection` — gatling-core/src/main/scala/io/gatling/core/controller/inject/open/OpenInjectionStep.scala — ramps users linearly over duration
- `ClosedInjectionStep` — gatling-core-java/src/main/java/io/gatling/javaapi/core/ClosedInjectionStep.java — closed-model injection step definition
