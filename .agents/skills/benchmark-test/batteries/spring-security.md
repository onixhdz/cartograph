# Spring Security Query Battery — Grounded Expected Symbols

All symbols verified against `spring-projects/spring-security` source and cartograph graph output (2026-04-30).

## Investigation 1: Authentication flow (9 symbols)

Query keyword: `"authentication manager provider filter success failure security context session"`
Query intent: `"how does spring security authenticate a request and persist the authenticated security context"`

Expected symbols:
- `ProviderManager` — core/src/main/java/org/springframework/security/authentication/ProviderManager.java — authenticates requests by delegating to registered `AuthenticationProvider`s and an optional parent manager
- `AuthenticationFilter` — web/src/main/java/org/springframework/security/web/authentication/AuthenticationFilter.java — request-driven authentication filter that converts input and delegates to the authentication manager
- `AbstractAuthenticationProcessingFilter` — web/src/main/java/org/springframework/security/web/authentication/AbstractAuthenticationProcessingFilter.java — base servlet authentication filter with success and failure control flow
- `UsernamePasswordAuthenticationFilter` — web/src/main/java/org/springframework/security/web/authentication/UsernamePasswordAuthenticationFilter.java — classic form-login filter that creates username/password authentication tokens
- `HttpSessionSecurityContextRepository` — web/src/main/java/org/springframework/security/web/context/HttpSessionSecurityContextRepository.java — persists the authenticated `SecurityContext` in the HTTP session
- `SecurityContextHolderFilter` — web/src/main/java/org/springframework/security/web/context/SecurityContextHolderFilter.java — loads and clears the per-request security context around servlet execution
- `DefaultAuthenticationEventPublisher` — core/src/main/java/org/springframework/security/authentication/DefaultAuthenticationEventPublisher.java — publishes authentication success and failure events
- `SavedRequestAwareAuthenticationSuccessHandler` — web/src/main/java/org/springframework/security/web/authentication/SavedRequestAwareAuthenticationSuccessHandler.java — redirects to the originally requested URL after successful login
- `SimpleUrlAuthenticationFailureHandler` — web/src/main/java/org/springframework/security/web/authentication/SimpleUrlAuthenticationFailureHandler.java — handles authentication failures with redirects or error responses

## Investigation 2: Authorization and access decisions (9 symbols)

Query keyword: `"authorization access decision voter interceptor privilege evaluator filter"`
Query intent: `"how does spring security make authorization and access-control decisions"`

Expected symbols:
- `AuthorizationManager` — core/src/main/java/org/springframework/security/authorization/AuthorizationManager.java — core interface for authorization decisions
- `AuthorizationFilter` — web/src/main/java/org/springframework/security/web/access/intercept/AuthorizationFilter.java — servlet filter that enforces request authorization
- `AuthorizationManagerWebInvocationPrivilegeEvaluator` — web/src/main/java/org/springframework/security/web/access/AuthorizationManagerWebInvocationPrivilegeEvaluator.java — evaluates whether a web invocation is authorized through an `AuthorizationManager`
- `AuthorizationManagerBeforeMethodInterceptor` — core/src/main/java/org/springframework/security/authorization/method/AuthorizationManagerBeforeMethodInterceptor.java — method-security interceptor that checks authorization before invocation
- `MethodSecurityInterceptor` — access/src/main/java/org/springframework/security/access/intercept/aopalliance/MethodSecurityInterceptor.java — classic AOP Alliance interceptor for secured methods
- `AccessDecisionManager` — access/src/main/java/org/springframework/security/access/AccessDecisionManager.java — legacy strategy interface for deciding access
- `AccessDecisionVoter` — access/src/main/java/org/springframework/security/access/AccessDecisionVoter.java — participant interface used by vote-based access decisions
- `AffirmativeBased` — access/src/main/java/org/springframework/security/access/vote/AffirmativeBased.java — access-decision implementation that grants when any voter approves
- `AbstractAccessDecisionManager` — access/src/main/java/org/springframework/security/access/vote/AbstractAccessDecisionManager.java — shared base implementation coordinating access-decision voters

## Investigation 3: Filter chain and request handling (8 symbols)

Query keyword: `"filter chain proxy request matcher firewall decorator async security"`
Query intent: `"how does spring security choose a filter chain and run security filters for an incoming servlet request"`

Expected symbols:
- `FilterChainProxy` — web/src/main/java/org/springframework/security/web/FilterChainProxy.java — main servlet entry point that chooses and invokes the matching security chain
- `DefaultSecurityFilterChain` — web/src/main/java/org/springframework/security/web/DefaultSecurityFilterChain.java — concrete filter-chain definition bound to a request matcher
- `SecurityFilterChain` — web/src/main/java/org/springframework/security/web/SecurityFilterChain.java — interface representing a matcher-backed security filter chain
- `FilterChainDecorator` — web/src/main/java/org/springframework/security/web/FilterChainProxy.java — extension interface for decorating filter-chain execution
- `ObservationFilterChainDecorator` — web/src/main/java/org/springframework/security/web/ObservationFilterChainDecorator.java — wraps filter-chain execution with observation instrumentation
- `StrictHttpFirewall` — web/src/main/java/org/springframework/security/web/firewall/StrictHttpFirewall.java — rejects or normalizes unsafe requests before chain processing
- `RequestMatcherRedirectFilter` — web/src/main/java/org/springframework/security/web/RequestMatcherRedirectFilter.java — redirects requests when a configured matcher applies
- `WebAsyncManagerIntegrationFilter` — web/src/main/java/org/springframework/security/web/context/request/async/WebAsyncManagerIntegrationFilter.java — propagates security context into async servlet execution

## Investigation 4: Security configuration and builder setup (8 symbols)

Query keyword: `"HttpSecurity WebSecurity builder configurer customizer filter chain build"`
Query intent: `"how does spring security wire HttpSecurity and WebSecurity builders into the final filter chain"`

Expected symbols:
- `HttpSecurityConfiguration` — config/src/main/java/org/springframework/security/config/annotation/web/configuration/HttpSecurityConfiguration.java — creates and prepares the `HttpSecurity` builder bean
- `WebSecurityConfiguration` — config/src/main/java/org/springframework/security/config/annotation/web/configuration/WebSecurityConfiguration.java — assembles the final `springSecurityFilterChain` and discovered configurers
- `HttpSecurity` — config/src/main/java/org/springframework/security/config/annotation/web/builders/HttpSecurity.java — builder for servlet security settings and filters
- `WebSecurity` — config/src/main/java/org/springframework/security/config/annotation/web/builders/WebSecurity.java — top-level builder composing one or more security filter chains
- `applyDefaultConfigurers` — config/src/main/java/org/springframework/security/config/annotation/web/configuration/HttpSecurityConfiguration.java — loads default `HttpSecurity` configurers from Spring factories
- `applyHttpSecurityCustomizers` — config/src/main/java/org/springframework/security/config/annotation/web/configuration/HttpSecurityConfiguration.java — applies `Customizer<HttpSecurity>` beans discovered in the application context
- `setFilterChainProxySecurityConfigurer` — config/src/main/java/org/springframework/security/config/annotation/web/configuration/WebSecurityConfiguration.java — finds and applies `WebSecurityConfigurer` implementations
- `addSecurityFilterChainBuilder` — config/src/main/java/org/springframework/security/config/annotation/web/builders/WebSecurity.java — registers security filter-chain builders before the final build step

## Investigation 5: Method security advisor wiring (8 symbols)

Query keyword: `"method security advisor interceptor preauthorize postauthorize bean parser"`
Query intent: `"how does spring security turn method-security configuration into authorization advisors and interceptors"`

Expected symbols:
- `AuthorizationAdvisorProxyFactory` — core/src/main/java/org/springframework/security/authorization/method/AuthorizationAdvisorProxyFactory.java — creates authorization advisors for proxied secured methods
- `AuthorizationManagerBeforeMethodInterceptor` — core/src/main/java/org/springframework/security/authorization/method/AuthorizationManagerBeforeMethodInterceptor.java — pre-invocation method authorization interceptor
- `AuthorizationManagerAfterMethodInterceptor` — core/src/main/java/org/springframework/security/authorization/method/AuthorizationManagerAfterMethodInterceptor.java — post-invocation method authorization interceptor
- `AuthorizeReturnObjectMethodInterceptor` — core/src/main/java/org/springframework/security/authorization/method/AuthorizeReturnObjectMethodInterceptor.java — secures return objects produced by authorized methods
- `AuthorizationManagerBeforeReactiveMethodInterceptor` — core/src/main/java/org/springframework/security/authorization/method/AuthorizationManagerBeforeReactiveMethodInterceptor.java — reactive pre-invocation method authorization interceptor
- `AuthorizationManagerAfterReactiveMethodInterceptor` — core/src/main/java/org/springframework/security/authorization/method/AuthorizationManagerAfterReactiveMethodInterceptor.java — reactive post-invocation method authorization interceptor
- `MethodSecurityBeanDefinitionParser` — config/src/main/java/org/springframework/security/config/method/MethodSecurityBeanDefinitionParser.java — parses modern method-security configuration into infrastructure beans
- `GlobalMethodSecurityBeanDefinitionParser` — config/src/main/java/org/springframework/security/config/method/GlobalMethodSecurityBeanDefinitionParser.java — parses legacy global method-security configuration into infrastructure beans
