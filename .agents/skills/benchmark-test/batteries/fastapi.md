# FastAPI Query Battery — Grounded Expected Symbols

All symbols verified against fastapi/fastapi source on GitHub (2026-04-05).

## Investigation 1: Route registration (8 symbols)

Query keyword: `"route endpoint path decorator GET POST handler"`
Query intent: `"how are HTTP routes registered and dispatched"`

Expected symbols:
- `APIRoute` — fastapi/routing.py — Route class wrapping Starlette Route
- `APIRouter` — fastapi/routing.py — Router that groups APIRoutes
- `get_request_handler` — fastapi/routing.py — builds the ASGI request handler
- `serialize_response` — fastapi/routing.py — serializes endpoint return value
- `FastAPI` — fastapi/applications.py — main application class
- `Path` — fastapi/param_functions.py — path parameter declaration
- `post` — fastapi/applications.py — @app.post decorator method
- `_extract_endpoint_context` — fastapi/routing.py — extracts context from endpoint

## Investigation 2: Dependency injection (8 symbols)

Query keyword: `"dependency injection Depends security parameter resolve"`
Query intent: `"how does dependency injection work"`

Expected symbols:
- `Depends` — fastapi/param_functions.py — Depends() function shortcut
- `Depends` — fastapi/params.py — Depends class definition
- `Security` — fastapi/param_functions.py — Security() function shortcut
- `Dependant` — fastapi/dependencies/models.py — dependency tree model
- `get_dependant` — fastapi/dependencies/utils.py — builds dependency tree from endpoint
- `solve_dependencies` — fastapi/dependencies/utils.py — resolves dependencies at request time
- `get_flat_dependant` — fastapi/dependencies/utils.py — flattens dependency tree
- `SecurityScopes` — fastapi/security/oauth2.py — OAuth2 security scopes

## Investigation 3: Request validation (8 symbols)

Query keyword: `"request body model validation pydantic schema field"`
Query intent: `"how are request bodies validated and parsed"`

Expected symbols:
- `Body` — fastapi/param_functions.py — Body() parameter function
- `RequestBody` — fastapi/openapi/models.py — OpenAPI request body model
- `RequestValidationError` — fastapi/exceptions.py — validation error class
- `get_openapi_operation_request_body` — fastapi/openapi/utils.py — builds request body spec
- `ModelField` — fastapi/_compat/v2.py — pydantic model field wrapper
- `analyze_param` — fastapi/dependencies/utils.py — analyzes endpoint parameter types
- `validate` — fastapi/_compat/v2.py — validation method
- `validation_exception_handler` — docs_src/handling_errors/tutorial005_py310.py — example handler

## Investigation 4: Middleware and exceptions (8 symbols)

Query keyword: `"middleware CORS exception handler error response build"`
Query intent: `"how are middleware and exception handlers configured"`

Expected symbols:
- `HTTPException` — fastapi/exceptions.py — HTTP error exception
- `WebSocketException` — fastapi/exceptions.py — WebSocket error exception
- `FastAPIError` — fastapi/exceptions.py — base FastAPI error
- `ValidationException` — fastapi/exceptions.py — validation exception
- `middleware` — fastapi/applications.py — @app.middleware decorator
- `build_middleware_stack` — fastapi/applications.py — builds ASGI middleware chain
- `validation_exception_handler` — docs_src/handling_errors/tutorial005_py310.py — example handler
- `ValidationErrorLoggingRoute` — docs_src/custom_request_and_route/tutorial002_py310.py — custom route

## Investigation 5: OpenAPI schema generation (8 symbols)

Query keyword: `"openapi schema docs swagger JSON generate operation"`
Query intent: `"how is the OpenAPI schema generated"`

Expected symbols:
- `get_openapi` — fastapi/openapi/utils.py — main OpenAPI schema builder
- `openapi` — fastapi/applications.py — FastAPI.openapi() method
- `OpenAPI` — fastapi/openapi/models.py — OpenAPI root model
- `Schema` — fastapi/openapi/models.py — JSON Schema model
- `GenerateJsonSchema` — fastapi/_compat/v2.py — pydantic JSON schema generator
- `get_openapi_operation_request_body` — fastapi/openapi/utils.py — builds operation request body
- `Components` — fastapi/openapi/models.py — OpenAPI components model
- `get_definitions` — fastapi/_compat/v2.py — gets JSON schema definitions
