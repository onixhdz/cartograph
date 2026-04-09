# Spring Framework Query Battery — Grounded Expected Symbols

All symbols verified against `spring-projects/spring-framework` source and cartograph graph output (2026-04-30).

## Investigation 1: Bean creation and autowiring (6 symbols)

Query keyword: `"bean factory autowire dependency constructor resolver post processor"`
Query intent: `"how does DefaultListableBeanFactory create beans and resolve autowired constructor or field dependencies"`

Expected symbols:
- `DefaultListableBeanFactory` — spring-beans/src/main/java/org/springframework/beans/factory/support/DefaultListableBeanFactory.java — central bean factory implementation that resolves bean definitions and dependency candidates
- `AbstractAutowireCapableBeanFactory` — spring-beans/src/main/java/org/springframework/beans/factory/support/AbstractAutowireCapableBeanFactory.java — core bean-creation implementation that instantiates, populates, and initializes beans
- `ConstructorResolver` — spring-beans/src/main/java/org/springframework/beans/factory/support/ConstructorResolver.java — resolves constructor and factory-method arguments during bean creation
- `DependencyDescriptor` — spring-beans/src/main/java/org/springframework/beans/factory/config/DependencyDescriptor.java — describes an injection point when resolving autowired dependencies
- `AutowiredAnnotationBeanPostProcessor` — spring-beans/src/main/java/org/springframework/beans/factory/annotation/AutowiredAnnotationBeanPostProcessor.java — processes `@Autowired` and related annotations on fields and methods
- `BeanDefinitionValueResolver` — spring-beans/src/main/java/org/springframework/beans/factory/support/BeanDefinitionValueResolver.java — turns bean-definition metadata into concrete runtime values during wiring

## Investigation 2: Spring MVC request dispatch (6 symbols)

Query keyword: `"DispatcherServlet handler mapping adapter handler execution chain request response body"`
Query intent: `"how does DispatcherServlet map an HTTP request to a controller method and write the response body"`

Expected symbols:
- `DispatcherServlet` — spring-webmvc/src/main/java/org/springframework/web/servlet/DispatcherServlet.java — main MVC front controller that dispatches servlet requests
- `HandlerExecutionChain` — spring-webmvc/src/main/java/org/springframework/web/servlet/HandlerExecutionChain.java — bundles the chosen handler with interceptors for a request
- `RequestMappingHandlerMapping` — spring-webmvc/src/main/java/org/springframework/web/servlet/mvc/method/annotation/RequestMappingHandlerMapping.java — maps annotated controller methods to requests
- `RequestMappingHandlerAdapter` — spring-webmvc/src/main/java/org/springframework/web/servlet/mvc/method/annotation/RequestMappingHandlerAdapter.java — invokes annotated controller methods
- `InvocableHandlerMethod` — spring-web/src/main/java/org/springframework/web/method/support/InvocableHandlerMethod.java — resolves arguments and invokes the selected handler method
- `RequestResponseBodyMethodProcessor` — spring-webmvc/src/main/java/org/springframework/web/servlet/mvc/method/annotation/RequestResponseBodyMethodProcessor.java — reads request bodies and writes response bodies for `@RequestBody` / `@ResponseBody`

## Investigation 3: Application context refresh and events (6 symbols)

Query keyword: `"application context refresh publish event lifecycle multicaster listener"`
Query intent: `"how does AbstractApplicationContext refresh the container and publish lifecycle events"`

Expected symbols:
- `AbstractApplicationContext` — spring-context/src/main/java/org/springframework/context/support/AbstractApplicationContext.java — core application-context base class that implements refresh orchestration
- `AnnotationConfigApplicationContext` — spring-context/src/main/java/org/springframework/context/annotation/AnnotationConfigApplicationContext.java — annotation-driven application context commonly used for Java configuration
- `ApplicationEventPublisher` — spring-context/src/main/java/org/springframework/context/ApplicationEventPublisher.java — interface for publishing application events
- `ContextRefreshedEvent` — spring-context/src/main/java/org/springframework/context/event/ContextRefreshedEvent.java — event published when an application context has been refreshed
- `SimpleApplicationEventMulticaster` — spring-context/src/main/java/org/springframework/context/event/SimpleApplicationEventMulticaster.java — default multicaster that dispatches events to listeners
- `DefaultLifecycleProcessor` — spring-context/src/main/java/org/springframework/context/support/DefaultLifecycleProcessor.java — coordinates start/stop lifecycle callbacks during context refresh and shutdown

## Investigation 4: Configuration parsing and component scanning (6 symbols)

Query keyword: `"configuration class parser import selector component scan bean definition"`
Query intent: `"how does spring parse configuration classes imports and component scanning into bean definitions"`

Expected symbols:
- `ConfigurationClassParser` — spring-context/src/main/java/org/springframework/context/annotation/ConfigurationClassParser.java — parses `@Configuration` classes, imports, and discovered metadata
- `ConfigurationClassBeanDefinitionReader` — spring-context/src/main/java/org/springframework/context/annotation/ConfigurationClassBeanDefinitionReader.java — turns parsed configuration classes into bean definitions
- `ComponentScanAnnotationParser` — spring-context/src/main/java/org/springframework/context/annotation/ComponentScanAnnotationParser.java — parses `@ComponentScan` declarations into scanned candidates
- `ComponentScanBeanDefinitionParser` — spring-context/src/main/java/org/springframework/context/annotation/ComponentScanBeanDefinitionParser.java — XML parser that configures component scanning and scanner options
- `ConditionEvaluator` — spring-context/src/main/java/org/springframework/context/annotation/ConditionEvaluator.java — evaluates `@Conditional` checks while processing configuration classes
- `DeferredImportSelectorHandler` — spring-context/src/main/java/org/springframework/context/annotation/ConfigurationClassParser.java — coordinates deferred import selectors after initial configuration parsing
