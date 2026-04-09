package query

import (
	"strings"
	"testing"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/service"
)

const (
	authFlowQuery       = "how does spring security authenticate a request and persist the authenticated security context"
	configBuildersQuery = "how does spring security wire HttpSecurity and WebSecurity builders into the final filter chain"
	dispatchQuery       = "how does DispatcherServlet map an HTTP request to a controller method and write the response body"
	lifecycleQuery      = "how does AbstractApplicationContext refresh the container and publish lifecycle events"
)

func TestContextBoost_ConfigBuilderSignals(t *testing.T) {
	queryText := configBuildersQuery
	sm := service.SymbolMatch{
		Name:     "applyDefaultConfigurers",
		FilePath: "config/src/main/java/org/springframework/security/config/annotation/web/configuration/HttpSecurityConfiguration.java",
	}

	got := contextBoost(queryText, sm)
	if got <= 1.0 {
		t.Fatalf("expected config symbol boost > 1.0, got %f", got)
	}
}

func TestContextBoost_SecuritySignals(t *testing.T) {
	queryText := authFlowQuery
	sm := service.SymbolMatch{
		Name:     "SecurityContextHolderFilter",
		FilePath: "web/src/main/java/org/springframework/security/web/context/SecurityContextHolderFilter.java",
	}

	got := contextBoost(queryText, sm)
	if got <= 1.0 {
		t.Fatalf("expected security symbol boost > 1.0, got %f", got)
	}
}

func TestContextBoost_PrefersCloserOverlap(t *testing.T) {
	queryText := configBuildersQuery
	configSymbol := service.SymbolMatch{
		Name:     "addSecurityFilterChainBuilder",
		FilePath: "config/src/main/java/org/springframework/security/config/annotation/web/builders/WebSecurity.java",
	}
	unrelatedSymbol := service.SymbolMatch{
		Name:     "PersistentTokenBasedRememberMeServices",
		FilePath: "web/src/main/java/org/springframework/security/web/authentication/rememberme/PersistentTokenBasedRememberMeServices.java",
	}

	configBoost := contextBoost(queryText, configSymbol)
	unrelatedBoost := contextBoost(queryText, unrelatedSymbol)
	if configBoost <= unrelatedBoost {
		t.Fatalf("expected config boost (%f) > unrelated boost (%f)", configBoost, unrelatedBoost)
	}
}

func TestProcessBoost_ConfigProcessSignals(t *testing.T) {
	queryText := configBuildersQuery

	got := processBoost(
		queryText,
		"builders.HttpSecurity.performBuild-flow",
		"Configuration",
		"method:HttpSecurity.performBuild",
	)
	if got <= 1.0 {
		t.Fatalf("expected config process boost > 1.0, got %f", got)
	}
}

func TestProcessBoost_PrefersSpecificProcessOverlap(t *testing.T) {
	queryText := configBuildersQuery

	specific := processBoost(
		queryText,
		"builders.HttpSecurity.performBuild-flow",
		"Configuration",
		"method:HttpSecurity.performBuild",
	)
	generic := processBoost(
		queryText,
		"authorization.Builder.validateURL-flow",
		"Validation",
		"method:Builder.validateURL",
	)
	if specific <= generic {
		t.Fatalf("expected specific process boost (%f) > generic boost (%f)", specific, generic)
	}
}

func TestTokenOverlapBoost_UsesSignatureAndCanonicalTokens(t *testing.T) {
	queryText := authFlowQuery
	sm := service.SymbolMatch{
		Name:      "successfulAuthentication",
		FilePath:  "web/src/main/java/org/springframework/security/web/authentication/AuthenticationFilter.java",
		Signature: "private void successfulAuthentication(HttpServletRequest request, HttpServletResponse response, FilterChain chain, Authentication authentication)",
	}

	got := tokenOverlapBoost(queryText, sm)
	if got <= 1.15 {
		t.Fatalf("expected signature overlap boost > 1.15, got %f", got)
	}
}

func TestContextBoost_AuthFlowPrefersFilterShape(t *testing.T) {
	queryText := authFlowQuery

	filterSymbol := service.SymbolMatch{
		Name:      "AuthenticationFilter",
		FilePath:  "web/src/main/java/org/springframework/security/web/authentication/AuthenticationFilter.java",
		Signature: "public class AuthenticationFilter",
	}
	wrapperSymbol := service.SymbolMatch{
		Name:      "Servlet3SecurityContextHolderAwareRequestWrapper",
		FilePath:  "web/src/main/java/org/springframework/security/web/servletapi/HttpServlet3RequestFactory.java",
		Signature: "void login(String username, String password)",
	}

	filterBoost := contextBoost(queryText, filterSymbol)
	wrapperBoost := contextBoost(queryText, wrapperSymbol)
	if filterBoost <= wrapperBoost {
		t.Fatalf("expected auth-flow filter boost (%f) > wrapper boost (%f)", filterBoost, wrapperBoost)
	}
}

func TestExpandIntentQuery_AuthFlowAddsFrameworkTerms(t *testing.T) {
	queryText := authFlowQuery
	got := expandIntentQuery(queryText)
	if got == queryText {
		t.Fatal("expected auth-flow query to be expanded")
	}
	for _, want := range []string{"filter", "provider", "manager", "repository", "handler"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected expanded query %q to contain %q", got, want)
		}
	}
}

func TestExpandIntentQuery_DispatchAddsMvcTerms(t *testing.T) {
	queryText := dispatchQuery
	got := expandIntentQuery(queryText)
	if got == queryText {
		t.Fatal("expected dispatch query to be expanded")
	}
	for _, want := range []string{"RequestMappingHandlerMapping", "RequestMappingHandlerAdapter", "RequestResponseBodyMethodProcessor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected expanded query %q to contain %q", got, want)
		}
	}
}

func TestExpandIntentQuery_LifecycleAddsEventTerms(t *testing.T) {
	queryText := lifecycleQuery
	got := expandIntentQuery(queryText)
	if got == queryText {
		t.Fatal("expected lifecycle query to be expanded")
	}
	for _, want := range []string{"ContextRefreshedEvent", "DefaultLifecycleProcessor", "AnnotationConfigApplicationContext"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected expanded query %q to contain %q", got, want)
		}
	}
}

func TestLabelBoost_AuthFlowPrefersClasses(t *testing.T) {
	queryText := authFlowQuery
	classBoost := labelBoost(queryText, service.SymbolMatch{
		Name:  "AuthenticationFilter",
		Label: string(graph.LabelClass),
	})
	methodBoost := labelBoost(queryText, service.SymbolMatch{
		Name:  "setAuthenticationFailureHandler",
		Label: string(graph.LabelMethod),
	})
	if classBoost <= methodBoost {
		t.Fatalf("expected class boost (%f) > method boost (%f)", classBoost, methodBoost)
	}
}

func TestLabelBoost_DispatchPrefersClasses(t *testing.T) {
	queryText := dispatchQuery
	classBoost := labelBoost(queryText, service.SymbolMatch{
		Name:  "RequestMappingHandlerAdapter",
		Label: string(graph.LabelClass),
	})
	methodBoost := labelBoost(queryText, service.SymbolMatch{
		Name:  "beforeBodyWrite",
		Label: string(graph.LabelMethod),
	})
	if classBoost <= methodBoost {
		t.Fatalf("expected class boost (%f) > method boost (%f)", classBoost, methodBoost)
	}
}

func TestLabelBoost_LifecyclePrefersClasses(t *testing.T) {
	queryText := lifecycleQuery
	classBoost := labelBoost(queryText, service.SymbolMatch{
		Name:  "DefaultLifecycleProcessor",
		Label: string(graph.LabelClass),
	})
	methodBoost := labelBoost(queryText, service.SymbolMatch{
		Name:  "publishEvent",
		Label: string(graph.LabelMethod),
	})
	if classBoost <= methodBoost {
		t.Fatalf("expected class boost (%f) > method boost (%f)", classBoost, methodBoost)
	}
}
