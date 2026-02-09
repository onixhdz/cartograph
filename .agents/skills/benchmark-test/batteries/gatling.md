# Gatling Query Battery — Grounded Expected Symbols

All symbols verified against gatling/gatling source on GitHub (2026-04-16).

## Investigation 1: Simulation execution (8 symbols)

Query keyword: `"simulation runner scenario setup injection controller configuration"`
Query intent: `"how does gatling run a load test simulation"`

Expected symbols:
- `Runner` — gatling-app/src/main/scala/io/gatling/app/Runner.scala — orchestrates full simulation lifecycle: setup, inject, throttle, run
- `Simulation` — gatling-core/src/main/scala/io/gatling/core/scenario/Simulation.scala — abstract base class users extend to define load tests
- `SimulationParams` — gatling-core/src/main/scala/io/gatling/core/scenario/Simulation.scala — parameters extracted from simulation setUp method
- `ScenarioBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/ScenarioBuilder.java — builds scenario definitions with action chains
- `PopulationBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/PopulationBuilder.java — builds user populations with injection profiles
- `OpenInjectionStep` — gatling-core-java/src/main/java/io/gatling/javaapi/core/OpenInjectionStep.java — defines open-model injection steps (ramp, constant rate)
- `Controller` — gatling-core/src/main/scala/io/gatling/core/controller/Controller.scala — Akka actor coordinating injection and throttling
- `GatlingConfiguration` — gatling-core/src/main/scala/io/gatling/core/config/GatlingConfiguration.scala — loads and holds all configuration settings

## Investigation 2: HTTP protocol (8 symbols)

Query keyword: `"http request response protocol engine listener client check"`
Query intent: `"how does the HTTP protocol handle requests and responses"`

Expected symbols:
- `HttpRequestBuilder` — gatling-http/src/main/scala/io/gatling/http/request/builder/HttpRequestBuilder.scala — builds HTTP request action definitions
- `HttpRequestExpressionBuilder` — gatling-http/src/main/scala/io/gatling/http/request/builder/HttpRequestExpressionBuilder.scala — builds HTTP requests with Expression Language support
- `HttpEngine` — gatling-http/src/main/scala/io/gatling/http/engine/HttpEngine.scala — manages HTTP client connections and pooling
- `GatlingHttpListener` — gatling-http/src/main/scala/io/gatling/http/engine/GatlingHttpListener.scala — Netty async listener handling HTTP responses
- `ResponseProcessor` — gatling-http/src/main/scala/io/gatling/http/engine/response/ResponseProcessor.scala — processes and validates HTTP responses
- `HttpProtocolBuilder` — gatling-http/src/main/scala/io/gatling/http/protocol/HttpProtocolBuilder.scala — builds HTTP protocol configuration
- `HttpCheckSupport` — gatling-http/src/main/scala/io/gatling/http/check/HttpCheckSupport.scala — trait providing HTTP check DSL methods
- `DefaultHttpClient` — gatling-http-client/src/main/java/io/gatling/http/client/impl/DefaultHttpClient.java — Netty-based HTTP client implementation

## Investigation 3: Session and statistics (8 symbols)

Query keyword: `"session stats data writer console log statistics engine"`
Query intent: `"how are session state and performance statistics tracked and written"`

Expected symbols:
- `Session` — gatling-core/src/main/scala/io/gatling/core/session/Session.scala — immutable virtual user state carrier
- `SessionAttribute` — gatling-core/src/main/scala/io/gatling/core/session/Session.scala — typed attribute accessor on session
- `DataWritersStatsEngine` — gatling-core/src/main/scala/io/gatling/core/stats/DataWritersStatsEngine.scala — stats engine dispatching to multiple data writers
- `ConsoleDataWriter` — gatling-core/src/main/scala/io/gatling/core/stats/writer/ConsoleDataWriter.scala — renders live stats to console during simulation
- `LogFileData` — gatling-charts/src/main/scala/io/gatling/charts/stats/LogFileData.scala — in-memory representation of parsed simulation.log
- `LogFileParser` — gatling-charts/src/main/scala/io/gatling/charts/stats/LogFileReader.scala — parses simulation.log into in-memory data structures
- `newStatsEngine` — gatling-app/src/main/scala/io/gatling/app/Runner.scala — factory function creating DataWritersStatsEngine
- `logGroupEnd` — gatling-core/src/main/scala/io/gatling/core/stats/DataWritersStatsEngine.scala — records end timing for a request group

## Investigation 4: Action chain and data feeding (8 symbols)

Query keyword: `"action chain builder exec loop pause feeder structure"`
Query intent: `"how are action chains and data feeders built in the scenario DSL"`

Expected symbols:
- `ActionBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/ActionBuilder.java — base type for all action builders in the DSL
- `ChainBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/ChainBuilder.java — builds chains of actions composing a scenario
- `StructureBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/StructureBuilder.java — base builder providing exec/pause/loop DSL
- `Loops` — gatling-core/src/main/scala/io/gatling/core/structure/Loops.scala — DSL mixin for repeat/foreach/during loop constructs
- `Execs` — gatling-core-java/src/main/java/io/gatling/javaapi/core/exec/Execs.java — DSL mixin for exec operations on chains
- `FeederBuilder` — gatling-core-java/src/main/java/io/gatling/javaapi/core/FeederBuilder.java — configures data feeders with strategy and source
- `FeederSource` — gatling-core/src/main/scala/io/gatling/core/feeder/FeederSource.scala — abstraction for loading feeder data from files
- `BatchedSeparatedValuesFeeder` — gatling-core/src/main/scala/io/gatling/core/feeder/BatchedSeparatedValuesFeeder.scala — efficient CSV/TSV feeder with batched reading

## Investigation 5: Assertions and injection models (8 symbols)

Query keyword: `"assertion throttle injection ramp rate open closed step"`
Query intent: `"how do assertions and injection profiles control load testing"`

Expected symbols:
- `AssertionWithPath` — gatling-core/src/main/scala/io/gatling/core/assertion/AssertionBuilders.scala — assertion builder after path selection (global/details/forAll)
- `AssertionWithPathAndTimeMetric` — gatling-core/src/main/scala/io/gatling/core/assertion/AssertionBuilders.scala — builder after selecting time metric
- `AssertionWithPathAndTarget` — gatling-core/src/main/scala/io/gatling/core/assertion/AssertionBuilders.scala — builder after selecting target statistic
- `Throttler` — gatling-core/src/main/scala/io/gatling/core/controller/throttle/Throttler.scala — Akka actor enforcing request rate limits
- `Throttle` — gatling-core/src/main/scala/io/gatling/core/controller/throttle/Throttler.scala — throttle configuration message type
- `RampOpenInjection` — gatling-core/src/main/scala/io/gatling/core/controller/inject/open/OpenInjectionStep.scala — ramps users linearly over duration
- `ConstantRateOpenInjection` — gatling-core/src/main/scala/io/gatling/core/controller/inject/open/OpenInjectionStep.scala — constant rate injection step
- `ClosedInjectionStep` — gatling-core-java/src/main/java/io/gatling/javaapi/core/ClosedInjectionStep.java — closed-model injection step definition
