package extractors

import "github.com/odvcencio/gotreesitter/grammars"

// CanExtract returns true if the given language can have symbols extracted,
// either via a hand-crafted query in LanguageQueries or via an inferred
// tags query from the gotreesitter library.
func CanExtract(language string) bool {
	if _, ok := LanguageQueries[language]; ok {
		return true
	}
	entry := grammars.DetectLanguageByName(language)
	if entry == nil {
		return false
	}
	return grammars.ResolveTagsQuery(*entry) != ""
}

// LanguageQueries maps language names to tree-sitter S-expression queries.
var LanguageQueries = map[string]string{
	"go":         goQueries,
	"typescript": tsQueries,
	"javascript": jsQueries,
	"python":     pyQueries,
	"java":       javaQueries,
	"rust":       rustQueries,
	"cpp":        cppQueries,
	"c":          cQueries,
	"ruby":       rubyQueries,
	"php":        phpQueries,
	"kotlin":     ktQueries,
	"swift":      swiftQueries,
	"csharp":     csQueries,
}

const goQueries = "(function_declaration name: (identifier) @name) @definition.function\n" +
	"(method_declaration name: (field_identifier) @name) @definition.method\n" +
	"(type_declaration (type_spec name: (type_identifier) @name type: (struct_type))) @definition.struct\n" +
	"(type_declaration (type_spec name: (type_identifier) @name type: (interface_type))) @definition.interface\n" +
	"(type_declaration (type_spec name: (type_identifier) @name)) @definition.type\n" +
	// Go import: aliased (import ts "github.com/...") — captures both alias and path.
	"(import_declaration (import_spec name: (package_identifier) @import.alias path: (interpreted_string_literal) @import.source)) @import\n" +
	"(import_declaration (import_spec_list (import_spec name: (package_identifier) @import.alias path: (interpreted_string_literal) @import.source))) @import\n" +
	// Go import: non-aliased (import "github.com/...") — path only.
	"(import_declaration (import_spec !name path: (interpreted_string_literal) @import.source)) @import\n" +
	"(import_declaration (import_spec_list (import_spec !name path: (interpreted_string_literal) @import.source))) @import\n" +
	"(field_declaration_list (field_declaration name: (field_identifier) @name) @definition.property)\n" +
	"(call_expression function: (identifier) @call.name) @call\n" +
	"(call_expression function: (selector_expression operand: (_) @call.receiver field: (field_identifier) @call.name)) @call\n" +
	"(composite_literal type: (type_identifier) @call.name) @call\n" +
	// Go struct embedding heritage: field_declaration with type but no name.
	"(type_declaration (type_spec name: (type_identifier) @heritage.class type: (struct_type (field_declaration_list (field_declaration !name type: (type_identifier) @heritage.extends))))) @heritage\n" +
	"(assignment_statement left: (expression_list (selector_expression operand: (_) @assignment.receiver field: (field_identifier) @assignment.property)) right: (_)) @assignment\n" +
	// Go spawn: go f(), go s.method(), go pkg.Func()
	"(go_statement (call_expression function: (identifier) @spawn.name)) @spawn\n" +
	"(go_statement (call_expression function: (selector_expression operand: (_) @spawn.receiver field: (field_identifier) @spawn.name))) @spawn\n" +
	// Go delegate: identifier passed as function argument
	"(call_expression arguments: (argument_list (identifier) @delegate.target)) @delegate\n"

const tsQueries = "(class_declaration name: (type_identifier) @name) @definition.class\n" +
	"(interface_declaration name: (type_identifier) @name) @definition.interface\n" +
	"(function_declaration name: (identifier) @name) @definition.function\n" +
	"(method_definition name: (property_identifier) @name) @definition.method\n" +
	"(lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function))) @definition.function\n" +
	"(lexical_declaration (variable_declarator name: (identifier) @name value: (function_expression))) @definition.function\n" +
	"(export_statement declaration: (lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function)))) @definition.function\n" +
	"(export_statement declaration: (lexical_declaration (variable_declarator name: (identifier) @name value: (function_expression)))) @definition.function\n" +
	"(import_statement source: (string) @import.source) @import\n" +
	"(export_statement source: (string) @import.source) @import\n" +
	"(call_expression function: (identifier) @call.name) @call\n" +
	"(call_expression function: (member_expression property: (property_identifier) @call.name)) @call\n" +
	"(new_expression constructor: (identifier) @call.name) @call\n" +
	"(public_field_definition name: (property_identifier) @name) @definition.property\n" +
	"(class_declaration name: (type_identifier) @heritage.class (class_heritage (extends_clause value: (identifier) @heritage.extends))) @heritage\n" +
	"(class_declaration name: (type_identifier) @heritage.class (class_heritage (implements_clause (type_identifier) @heritage.implements))) @heritage.impl\n" +
	"(assignment_expression left: (member_expression object: (_) @assignment.receiver property: (property_identifier) @assignment.property) right: (_)) @assignment\n" +
	"(augmented_assignment_expression left: (member_expression object: (_) @assignment.receiver property: (property_identifier) @assignment.property) right: (_)) @assignment\n" +
	// TS spawn: setTimeout/setInterval/queueMicrotask with callback
	"(call_expression function: (identifier) @_fn (#match? @_fn \"^(setTimeout|setInterval|queueMicrotask|setImmediate)$\") arguments: (arguments (identifier) @spawn.target)) @spawn\n" +
	"(call_expression function: (identifier) @_fn (#match? @_fn \"^(setTimeout|setInterval|queueMicrotask|setImmediate)$\") arguments: (arguments (member_expression property: (property_identifier) @spawn.target))) @spawn\n" +
	// new Worker
	"(new_expression constructor: (identifier) @_ctor (#eq? @_ctor \"Worker\")) @spawn\n" +
	// TS spawn: addEventListener with handler
	"(call_expression function: (member_expression property: (property_identifier) @_fn (#eq? @_fn \"addEventListener\")) arguments: (arguments (_) (identifier) @spawn.target)) @spawn\n" +
	// TS spawn: Promise.then/catch/finally with handler
	"(call_expression function: (member_expression property: (property_identifier) @_fn (#match? @_fn \"^(then|catch|finally)$\")) arguments: (arguments (identifier) @delegate.target)) @delegate\n" +
	// TS delegate: identifier passed as function argument
	"(call_expression arguments: (arguments (identifier) @delegate.target)) @delegate\n"

const jsQueries = "(class_declaration name: (identifier) @name) @definition.class\n" +
	"(function_declaration name: (identifier) @name) @definition.function\n" +
	"(method_definition name: (property_identifier) @name) @definition.method\n" +
	"(lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function))) @definition.function\n" +
	"(lexical_declaration (variable_declarator name: (identifier) @name value: (function_expression))) @definition.function\n" +
	"(export_statement declaration: (lexical_declaration (variable_declarator name: (identifier) @name value: (arrow_function)))) @definition.function\n" +
	"(export_statement declaration: (lexical_declaration (variable_declarator name: (identifier) @name value: (function_expression)))) @definition.function\n" +
	"(import_statement source: (string) @import.source) @import\n" +
	"(export_statement source: (string) @import.source) @import\n" +
	"(call_expression function: (identifier) @call.name) @call\n" +
	"(call_expression function: (member_expression property: (property_identifier) @call.name)) @call\n" +
	"(new_expression constructor: (identifier) @call.name) @call\n" +
	"(field_definition property: (property_identifier) @name) @definition.property\n" +
	"(class_declaration name: (identifier) @heritage.class (class_heritage (identifier) @heritage.extends)) @heritage\n" +
	"(assignment_expression left: (member_expression object: (_) @assignment.receiver property: (property_identifier) @assignment.property) right: (_)) @assignment\n" +
	"(augmented_assignment_expression left: (member_expression object: (_) @assignment.receiver property: (property_identifier) @assignment.property) right: (_)) @assignment\n" +
	// JS spawn: setTimeout/setInterval/queueMicrotask with callback
	"(call_expression function: (identifier) @_fn (#match? @_fn \"^(setTimeout|setInterval|queueMicrotask|setImmediate)$\") arguments: (arguments (identifier) @spawn.target)) @spawn\n" +
	"(call_expression function: (identifier) @_fn (#match? @_fn \"^(setTimeout|setInterval|queueMicrotask|setImmediate)$\") arguments: (arguments (member_expression property: (property_identifier) @spawn.target))) @spawn\n" +
	// new Worker
	"(new_expression constructor: (identifier) @_ctor (#eq? @_ctor \"Worker\")) @spawn\n" +
	// JS delegate: identifier passed as function argument
	"(call_expression arguments: (arguments (identifier) @delegate.target)) @delegate\n"

const pyQueries = "(class_definition name: (identifier) @name) @definition.class\n" +
	"(function_definition name: (identifier) @name) @definition.function\n" +
	// Python method: function_definition nested inside class body
	"(class_definition body: (block (function_definition name: (identifier) @name) @definition.method))\n" +
	// Python method: decorated function inside class body (@staticmethod, @classmethod, etc.)
	"(class_definition body: (block (decorated_definition definition: (function_definition name: (identifier) @name)) @definition.method))\n" +
	// Python property: class-level annotated assignment (dataclass fields, Pydantic models)
	"(class_definition body: (block (expression_statement (assignment left: (identifier) @name)) @definition.property))\n" +
	// Python property: bare type annotation (e.g., name: str)
	"(class_definition body: (block (expression_statement (type (identifier) @name)) @definition.property))\n" +
	"(import_statement name: (dotted_name) @import.source) @import\n" +
	"(import_from_statement module_name: (dotted_name) @import.source) @import\n" +
	"(import_from_statement module_name: (relative_import) @import.source) @import\n" +
	"(call function: (identifier) @call.name) @call\n" +
	"(call function: (attribute attribute: (identifier) @call.name)) @call\n" +
	"(class_definition name: (identifier) @heritage.class superclasses: (argument_list (identifier) @heritage.extends)) @heritage\n" +
	"(assignment left: (attribute object: (_) @assignment.receiver attribute: (identifier) @assignment.property) right: (_)) @assignment\n" +
	"(augmented_assignment left: (attribute object: (_) @assignment.receiver attribute: (identifier) @assignment.property) right: (_)) @assignment\n" +
	// Python spawn: threading.Thread(target=fn), multiprocessing.Process(target=fn)
	"(call function: (attribute attribute: (identifier) @_fn (#match? @_fn \"^(Thread|Process)$\")) arguments: (argument_list (keyword_argument name: (identifier) @_kw (#eq? @_kw \"target\") value: (identifier) @spawn.target))) @spawn\n" +
	// asyncio.create_task, asyncio.ensure_future
	"(call function: (attribute attribute: (identifier) @_fn (#match? @_fn \"^(create_task|ensure_future)$\")) arguments: (argument_list (call function: (identifier) @spawn.target))) @spawn\n" +
	"(call function: (attribute attribute: (identifier) @_fn (#match? @_fn \"^(create_task|ensure_future)$\")) arguments: (argument_list (call function: (attribute attribute: (identifier) @spawn.target)))) @spawn\n" +
	// Python spawn: asyncio.run() / run_until_complete() — top-level async entry points
	"(call function: (attribute attribute: (identifier) @_fn (#match? @_fn \"^(run|run_until_complete)$\")) arguments: (argument_list (call function: (identifier) @spawn.target))) @spawn\n" +
	// Python spawn: run_in_threadpool(fn) — common in FastAPI/Starlette
	"(call function: (identifier) @_fn (#eq? @_fn \"run_in_threadpool\") arguments: (argument_list (identifier) @spawn.target)) @spawn\n" +
	// Python spawn: executor.submit(fn)
	"(call function: (attribute attribute: (identifier) @_fn (#eq? @_fn \"submit\")) arguments: (argument_list (identifier) @spawn.target)) @spawn\n" +
	// Python delegate: identifier passed as function argument
	"(call arguments: (argument_list (identifier) @delegate.target)) @delegate\n"

const javaQueries = "(class_declaration name: (identifier) @name) @definition.class\n" +
	"(interface_declaration name: (identifier) @name) @definition.interface\n" +
	"(enum_declaration name: (identifier) @name) @definition.enum\n" +
	"(annotation_type_declaration name: (identifier) @name) @definition.annotation\n" +
	"(method_declaration name: (identifier) @name) @definition.method\n" +
	"(constructor_declaration name: (identifier) @name) @definition.constructor\n" +
	"(field_declaration declarator: (variable_declarator name: (identifier) @name)) @definition.property\n" +
	"(import_declaration (_) @import.source) @import\n" +
	"(method_invocation name: (identifier) @call.name) @call\n" +
	"(method_invocation object: (_) name: (identifier) @call.name) @call\n" +
	"(object_creation_expression type: (type_identifier) @call.name) @call\n" +
	"(class_declaration name: (identifier) @heritage.class (superclass (type_identifier) @heritage.extends)) @heritage\n" +
	"(class_declaration name: (identifier) @heritage.class (super_interfaces (type_list (type_identifier) @heritage.implements))) @heritage.impl\n" +
	"(assignment_expression left: (field_access object: (_) @assignment.receiver field: (identifier) @assignment.property) right: (_)) @assignment\n" +
	// Java spawn: executor.submit(fn), executor.execute(fn), new Thread(fn).start()
	"(method_invocation name: (identifier) @_fn (#match? @_fn \"^(submit|execute|start)$\") arguments: (argument_list (identifier) @spawn.target)) @spawn\n" +
	"(method_invocation name: (identifier) @_fn (#match? @_fn \"^(submit|execute|start)$\") arguments: (argument_list (method_reference) @spawn.target)) @spawn\n" +
	// Java method reference: Type::method, Type::new (common in streams)
	"(method_reference (identifier) @call.name) @call\n" +
	"(method_reference . (type_identifier) @call.receiver \"::\" (identifier) @call.name) @call\n" +
	// Java delegate: identifier passed as method argument
	"(method_invocation arguments: (argument_list (identifier) @delegate.target)) @delegate\n"

const rustQueries = "(function_item name: (identifier) @name) @definition.function\n" +
	"(struct_item name: (type_identifier) @name) @definition.struct\n" +
	"(enum_item name: (type_identifier) @name) @definition.enum\n" +
	"(trait_item name: (type_identifier) @name) @definition.trait\n" +
	"(impl_item type: (type_identifier) @name !trait) @definition.impl\n" +
	"(mod_item name: (identifier) @name) @definition.module\n" +
	"(type_item name: (type_identifier) @name) @definition.type\n" +
	"(const_item name: (identifier) @name) @definition.const\n" +
	"(static_item name: (identifier) @name) @definition.static\n" +
	"(macro_definition name: (identifier) @name) @definition.macro\n" +
	"(use_declaration argument: (_) @import.source) @import\n" +
	"(call_expression function: (identifier) @call.name) @call\n" +
	"(call_expression function: (field_expression field: (field_identifier) @call.name)) @call\n" +
	"(call_expression function: (scoped_identifier name: (identifier) @call.name)) @call\n" +
	"(call_expression function: (generic_function function: (identifier) @call.name)) @call\n" +
	"(struct_expression name: (type_identifier) @call.name) @call\n" +
	"(field_declaration_list (field_declaration name: (field_identifier) @name) @definition.property)\n" +
	"(impl_item trait: (type_identifier) @heritage.trait type: (type_identifier) @heritage.class) @heritage\n" +
	"(impl_item trait: (generic_type type: (type_identifier) @heritage.trait) type: (type_identifier) @heritage.class) @heritage\n" +
	"(impl_item trait: (type_identifier) @heritage.trait type: (generic_type type: (type_identifier) @heritage.class)) @heritage\n" +
	"(impl_item trait: (generic_type type: (type_identifier) @heritage.trait) type: (generic_type type: (type_identifier) @heritage.class)) @heritage\n" +
	"(assignment_expression left: (field_expression value: (_) @assignment.receiver field: (field_identifier) @assignment.property) right: (_)) @assignment\n" +
	// Rust spawn: tokio::spawn(f()), std::thread::spawn(|| { }), rayon::spawn(f)
	"(call_expression function: (scoped_identifier name: (identifier) @_fn (#match? @_fn \"^(spawn|spawn_blocking)$\")) arguments: (arguments (call_expression function: (identifier) @spawn.target))) @spawn\n" +
	"(call_expression function: (scoped_identifier name: (identifier) @_fn (#match? @_fn \"^(spawn|spawn_blocking)$\")) arguments: (arguments (call_expression function: (field_expression field: (field_identifier) @spawn.target)))) @spawn\n" +
	"(call_expression function: (scoped_identifier name: (identifier) @_fn (#match? @_fn \"^(spawn|spawn_blocking)$\")) arguments: (arguments (call_expression function: (scoped_identifier name: (identifier) @spawn.target)))) @spawn\n" +
	// Rust delegate: identifier passed as function argument
	"(call_expression arguments: (arguments (identifier) @delegate.target)) @delegate\n"

const cQueries = "(function_definition declarator: (function_declarator declarator: (identifier) @name)) @definition.function\n" +
	"(declaration declarator: (function_declarator declarator: (identifier) @name)) @definition.function\n" +
	// Pointer-returning functions: int *foo() or int **foo()
	"(function_definition declarator: (pointer_declarator declarator: (function_declarator declarator: (identifier) @name))) @definition.function\n" +
	"(declaration declarator: (pointer_declarator declarator: (function_declarator declarator: (identifier) @name))) @definition.function\n" +
	"(function_definition declarator: (pointer_declarator declarator: (pointer_declarator declarator: (function_declarator declarator: (identifier) @name)))) @definition.function\n" +
	"(struct_specifier name: (type_identifier) @name body: (field_declaration_list)) @definition.struct\n" +
	"(struct_specifier name: (type_identifier) @name) @definition.struct\n" +
	"(enum_specifier name: (type_identifier) @name body: (enumerator_list)) @definition.enum\n" +
	"(enum_specifier name: (type_identifier) @name) @definition.enum\n" +
	"(type_definition declarator: (type_identifier) @name) @definition.typedef\n" +
	"(union_specifier name: (type_identifier) @name body: (field_declaration_list)) @definition.union\n" +
	"(union_specifier name: (type_identifier) @name) @definition.union\n" +
	"(preproc_include path: (_) @import.source) @import\n" +
	"(call_expression function: (identifier) @call.name) @call\n" +
	"(call_expression function: (field_expression field: (field_identifier) @call.name)) @call\n" +
	"(preproc_function_def name: (identifier) @name) @definition.macro\n" +
	"(preproc_def name: (identifier) @name) @definition.macro\n" +
	// Assignment: p.x = 10, p->y = 20
	"(assignment_expression left: (field_expression argument: (_) @assignment.receiver field: (field_identifier) @assignment.property) right: (_)) @assignment\n" +
	// C spawn: pthread_create(&tid, NULL, fn, arg)
	"(call_expression function: (identifier) @_fn (#eq? @_fn \"pthread_create\") arguments: (argument_list (_) (_) (identifier) @spawn.target)) @spawn\n" +
	// C delegate: identifier passed as function argument (function pointer passing)
	"(call_expression arguments: (argument_list (identifier) @delegate.target)) @delegate\n"

const cppQueries = "(function_definition declarator: (function_declarator declarator: (identifier) @name)) @definition.function\n" +
	"(function_definition declarator: (function_declarator declarator: (qualified_identifier name: (identifier) @name))) @definition.method\n" +
	// Pointer-returning functions/methods.
	"(function_definition declarator: (pointer_declarator declarator: (function_declarator declarator: (identifier) @name))) @definition.function\n" +
	"(function_definition declarator: (pointer_declarator declarator: (function_declarator declarator: (qualified_identifier name: (identifier) @name)))) @definition.method\n" +
	// Double-pointer return.
	"(function_definition declarator: (pointer_declarator declarator: (pointer_declarator declarator: (function_declarator declarator: (identifier) @name)))) @definition.function\n" +
	// Reference-returning functions/methods.
	"(function_definition declarator: (reference_declarator (function_declarator declarator: (identifier) @name))) @definition.function\n" +
	"(function_definition declarator: (reference_declarator (function_declarator declarator: (qualified_identifier name: (identifier) @name)))) @definition.method\n" +
	// Destructor methods.
	"(function_definition declarator: (function_declarator declarator: (qualified_identifier name: (destructor_name) @name))) @definition.method\n" +
	// Declaration prototypes (headers without body).
	"(declaration declarator: (function_declarator declarator: (identifier) @name)) @definition.function\n" +
	"(declaration declarator: (function_declarator declarator: (qualified_identifier name: (identifier) @name))) @definition.method\n" +
	// Field method declarations (inside class body).
	"(field_declaration declarator: (function_declarator declarator: (field_identifier) @name)) @definition.method\n" +
	"(class_specifier name: (type_identifier) @name body: (field_declaration_list)) @definition.class\n" +
	"(class_specifier name: (type_identifier) @name) @definition.class\n" +
	"(struct_specifier name: (type_identifier) @name body: (field_declaration_list)) @definition.struct\n" +
	"(struct_specifier name: (type_identifier) @name) @definition.struct\n" +
	"(enum_specifier name: (type_identifier) @name body: (enumerator_list)) @definition.enum\n" +
	"(enum_specifier name: (type_identifier) @name) @definition.enum\n" +
	"(union_specifier name: (type_identifier) @name body: (field_declaration_list)) @definition.union\n" +
	"(union_specifier name: (type_identifier) @name) @definition.union\n" +
	"(type_definition declarator: (type_identifier) @name) @definition.typedef\n" +
	"(namespace_definition name: (namespace_identifier) @name) @definition.namespace\n" +
	"(template_declaration (class_specifier name: (type_identifier) @name)) @definition.template\n" +
	// Template class specialization: template<> class Foo<int> { ... }
	"(template_declaration (class_specifier name: (template_type name: (type_identifier) @name))) @definition.template\n" +
	"(template_declaration (function_definition declarator: (function_declarator declarator: (identifier) @name))) @definition.template\n" +
	// Template function specialization: template<> int add<int>(...) { ... }
	"(template_declaration (function_definition declarator: (function_declarator declarator: (template_function name: (identifier) @name)))) @definition.template\n" +
	"(preproc_include path: (_) @import.source) @import\n" +
	"(call_expression function: (identifier) @call.name) @call\n" +
	"(call_expression function: (field_expression field: (field_identifier) @call.name)) @call\n" +
	"(call_expression function: (qualified_identifier name: (identifier) @call.name)) @call\n" +
	"(call_expression function: (template_function name: (identifier) @call.name)) @call\n" +
	// new expression constructor call.
	"(new_expression type: (type_identifier) @call.name) @call\n" +
	"(preproc_function_def name: (identifier) @name) @definition.macro\n" +
	"(class_specifier name: (type_identifier) @heritage.class (base_class_clause (type_identifier) @heritage.extends)) @heritage\n" +
	"(class_specifier name: (type_identifier) @heritage.class (base_class_clause (access_specifier) (type_identifier) @heritage.extends)) @heritage\n" +
	"(field_declaration declarator: (field_identifier) @name) @definition.property\n" +
	"(assignment_expression left: (field_expression argument: (_) @assignment.receiver field: (field_identifier) @assignment.property) right: (_)) @assignment\n" +
	// C++ spawn: std::thread(fn), std::async(policy, fn), std::jthread(fn)
	"(call_expression function: (qualified_identifier name: (identifier) @_fn (#match? @_fn \"^(thread|async|jthread)$\"))) @spawn\n" +
	// C++ delegate: identifier passed as function argument
	"(call_expression arguments: (argument_list (identifier) @delegate.target)) @delegate\n"

const rubyQueries = "(class name: (constant) @name) @definition.class\n" +
	"(module name: (constant) @name) @definition.module\n" +
	"(method name: (identifier) @name) @definition.method\n" +
	"(singleton_method name: (identifier) @name) @definition.function\n" +
	"(call method: (identifier) @call.name) @call\n" +
	"(class name: (constant) @heritage.class superclass: (superclass (constant) @heritage.extends)) @heritage\n" +
	// Ruby include/extend/prepend as heritage (mixin pattern)
	"(call method: (identifier) @_fn (#match? @_fn \"^(include|extend|prepend)$\") arguments: (argument_list (constant) @heritage.extends)) @heritage\n" +
	// Ruby attr_accessor/attr_reader/attr_writer as properties
	"(call method: (identifier) @_fn (#match? @_fn \"^(attr_accessor|attr_reader|attr_writer)$\") arguments: (argument_list (simple_symbol) @name)) @definition.property\n" +
	// Ruby spawn: Thread.new { } or Thread.new(&method(:fn))
	"(call receiver: (constant) @_recv (#eq? @_recv \"Thread\") method: (identifier) @_fn (#eq? @_fn \"new\")) @spawn\n" +
	// Ruby delegate: identifier passed as method argument
	"(call arguments: (argument_list (identifier) @delegate.target)) @delegate\n"

const phpQueries = "(namespace_definition name: (namespace_name) @name) @definition.namespace\n" +
	"(class_declaration name: (name) @name) @definition.class\n" +
	"(interface_declaration name: (name) @name) @definition.interface\n" +
	"(trait_declaration name: (name) @name) @definition.trait\n" +
	"(enum_declaration name: (name) @name) @definition.enum\n" +
	"(function_definition name: (name) @name) @definition.function\n" +
	"(method_declaration name: (name) @name) @definition.method\n" +
	"(namespace_use_declaration (namespace_use_clause (qualified_name) @import.source)) @import\n" +
	"(function_call_expression function: (name) @call.name) @call\n" +
	"(member_call_expression name: (name) @call.name) @call\n" +
	"(nullsafe_member_call_expression name: (name) @call.name) @call\n" +
	"(scoped_call_expression name: (name) @call.name) @call\n" +
	"(object_creation_expression (name) @call.name) @call\n" +
	"(class_declaration name: (name) @heritage.class (base_clause [(name) (qualified_name)] @heritage.extends)) @heritage\n" +
	"(class_declaration name: (name) @heritage.class (class_interface_clause [(name) (qualified_name)] @heritage.implements)) @heritage.impl\n" +
	"(class_declaration name: (name) @heritage.class body: (declaration_list (use_declaration [(name) (qualified_name)] @heritage.trait))) @heritage\n" +
	"(property_declaration (property_element (variable_name (name) @name))) @definition.property\n" +
	"(assignment_expression left: (member_access_expression object: (_) @assignment.receiver name: (name) @assignment.property) right: (_)) @assignment\n" +
	// PHP spawn: pcntl_fork()
	"(function_call_expression function: (name) @_fn (#eq? @_fn \"pcntl_fork\")) @spawn\n" +
	// PHP delegate: bare name passed as function argument
	"(function_call_expression arguments: (arguments (name) @delegate.target)) @delegate\n"

const ktQueries = "(class_declaration (type_identifier) @name) @definition.class\n" +
	"(object_declaration (type_identifier) @name) @definition.class\n" +
	// Companion object inside class body.
	"(companion_object (type_identifier) @name) @definition.class\n" +
	// Enum entries.
	"(enum_entry (simple_identifier) @name) @definition.const\n" +
	// Type alias.
	"(type_alias (type_identifier) @name) @definition.type\n" +
	"(function_declaration (simple_identifier) @name) @definition.function\n" +
	"(secondary_constructor) @definition.constructor\n" +
	"(import_header (identifier) @import.source) @import\n" +
	"(call_expression (simple_identifier) @call.name) @call\n" +
	"(call_expression (navigation_expression (simple_identifier) @call.name)) @call\n" +
	// Constructor invocation calls.
	"(constructor_invocation (user_type (type_identifier) @call.name)) @call\n" +
	// Heritage via delegation_specifier.
	"(class_declaration (type_identifier) @heritage.class (delegation_specifier (user_type (type_identifier) @heritage.extends))) @heritage\n" +
	"(class_declaration (type_identifier) @heritage.class (delegation_specifier (constructor_invocation (user_type (type_identifier) @heritage.extends)))) @heritage\n" +
	"(property_declaration (variable_declaration (simple_identifier) @name)) @definition.property\n" +
	// Kotlin spawn: launch { }, async { }, GlobalScope.launch { }
	"(call_expression (simple_identifier) @_fn (#match? @_fn \"^(launch|async)$\")) @spawn\n" +
	"(call_expression (navigation_expression (simple_identifier) @_fn (#match? @_fn \"^(launch|async)$\"))) @spawn\n" +
	// Kotlin delegate: identifier passed as function argument
	"(call_expression (value_arguments (value_argument (simple_identifier) @delegate.target))) @delegate\n"

const swiftQueries = "(class_declaration \"class\" name: (type_identifier) @name) @definition.class\n" +
	"(class_declaration \"struct\" name: (type_identifier) @name) @definition.struct\n" +
	"(class_declaration \"enum\" name: (type_identifier) @name) @definition.enum\n" +
	"(class_declaration \"extension\" name: (user_type (type_identifier) @name)) @definition.class\n" +
	"(class_declaration \"actor\" name: (type_identifier) @name) @definition.class\n" +
	"(protocol_declaration name: (type_identifier) @name) @definition.interface\n" +
	// Typealias definition.
	"(typealias_declaration name: (type_identifier) @name) @definition.type\n" +
	"(function_declaration name: (simple_identifier) @name) @definition.function\n" +
	"(protocol_function_declaration name: (simple_identifier) @name) @definition.method\n" +
	"(init_declaration) @definition.constructor\n" +
	"(property_declaration (pattern (simple_identifier) @name)) @definition.property\n" +
	"(import_declaration (identifier (simple_identifier) @import.source)) @import\n" +
	"(call_expression (simple_identifier) @call.name) @call\n" +
	"(call_expression (navigation_expression (navigation_suffix (simple_identifier) @call.name))) @call\n" +
	"(class_declaration name: (type_identifier) @heritage.class (inheritance_specifier inherits_from: (user_type (type_identifier) @heritage.extends))) @heritage\n" +
	"(protocol_declaration name: (type_identifier) @heritage.class (inheritance_specifier inherits_from: (user_type (type_identifier) @heritage.extends))) @heritage\n" +
	// Extension conformance heritage.
	"(class_declaration \"extension\" name: (user_type (type_identifier) @heritage.class) (inheritance_specifier inherits_from: (user_type (type_identifier) @heritage.extends))) @heritage\n" +
	// Swift spawn: Task { }, Task.detached { }, DispatchQueue.global().async { }
	"(call_expression (simple_identifier) @_fn (#eq? @_fn \"Task\")) @spawn\n" +
	// Swift delegate: identifier passed as function argument
	"(call_expression (value_arguments (value_argument (simple_identifier) @delegate.target))) @delegate\n"

const csQueries = "(class_declaration name: (identifier) @name) @definition.class\n" +
	"(interface_declaration name: (identifier) @name) @definition.interface\n" +
	"(struct_declaration name: (identifier) @name) @definition.struct\n" +
	"(enum_declaration name: (identifier) @name) @definition.enum\n" +
	"(record_declaration name: (identifier) @name) @definition.record\n" +
	"(delegate_declaration name: (identifier) @name) @definition.delegate\n" +
	"(namespace_declaration name: (identifier) @name) @definition.namespace\n" +
	"(namespace_declaration name: (qualified_name) @name) @definition.namespace\n" +
	// File-scoped namespace (C# 10+).
	"(file_scoped_namespace_declaration name: (identifier) @name) @definition.namespace\n" +
	"(file_scoped_namespace_declaration name: (qualified_name) @name) @definition.namespace\n" +
	"(method_declaration name: (identifier) @name) @definition.method\n" +
	"(local_function_statement name: (identifier) @name) @definition.function\n" +
	"(constructor_declaration name: (identifier) @name) @definition.constructor\n" +
	"(using_directive (qualified_name) @import.source) @import\n" +
	"(using_directive (identifier) @import.source) @import\n" +
	"(invocation_expression function: (identifier) @call.name) @call\n" +
	"(invocation_expression function: (member_access_expression name: (identifier) @call.name)) @call\n" +
	// Conditional access call (C# 6+): obj?.Method()
	"(invocation_expression function: (conditional_access_expression (member_binding_expression name: (identifier) @call.name))) @call\n" +
	"(object_creation_expression type: (identifier) @call.name) @call\n" +
	// Target-typed new (C# 9+): new(...)
	"(implicit_object_creation_expression) @call\n" +
	// Heritage: base_list contains identifier directly (no simple_base_type wrapper).
	"(class_declaration name: (identifier) @heritage.class (base_list (identifier) @heritage.extends)) @heritage\n" +
	"(class_declaration name: (identifier) @heritage.class (base_list (generic_name (identifier) @heritage.extends))) @heritage\n" +
	"(interface_declaration name: (identifier) @heritage.class (base_list (identifier) @heritage.extends)) @heritage\n" +
	"(struct_declaration name: (identifier) @heritage.class (base_list (identifier) @heritage.extends)) @heritage\n" +
	"(property_declaration name: (identifier) @name) @definition.property\n" +
	"(assignment_expression left: (member_access_expression expression: (_) @assignment.receiver name: (identifier) @assignment.property) right: (_)) @assignment\n" +
	// C# spawn: Task.Run(() => fn()), Task.Factory.StartNew(), ThreadPool.QueueUserWorkItem()
	"(invocation_expression function: (member_access_expression name: (identifier) @_fn (#match? @_fn \"^(Run|StartNew|QueueUserWorkItem)$\"))) @spawn\n" +
	// C# delegate: identifier passed as method argument
	"(invocation_expression arguments: (argument_list (argument (identifier) @delegate.target))) @delegate\n"
