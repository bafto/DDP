package compiler

import (
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/DDP-Projekt/Kompilierer/pkg/ast"
	"github.com/DDP-Projekt/Kompilierer/pkg/ddperror"
	"github.com/DDP-Projekt/Kompilierer/pkg/token"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/llir/irutil"
)

// small wrapper for a ast.FuncDecl and the corresponding ir function
type funcWrapper struct {
	irFunc   *ir.Func      // the function in the llvm ir
	funcDecl *ast.FuncDecl // the ast.FuncDecl
}

// holds state to compile a DDP AST into llvm ir
type Compiler struct {
	ast          *ast.Ast
	mod          *ir.Module       // the ir module (basically the ir file)
	errorHandler ddperror.Handler // errors are passed to this function
	result       *Result          // result of the compilation

	cbb          *ir.Block               // current basic block in the ir
	cf           *ir.Func                // current function
	scp          *scope                  // current scope in the ast (not in the ir)
	cfscp        *scope                  // out-most scope of the current function
	functions    map[string]*funcWrapper // all the global functions
	latestReturn value.Value             // return of the latest evaluated expression (in the ir)
}

// create a new Compiler to compile the passed AST
func New(Ast *ast.Ast, errorHandler ddperror.Handler) *Compiler {
	if errorHandler == nil { // default error handler does nothing
		errorHandler = ddperror.EmptyHandler
	}
	return &Compiler{
		ast:          Ast,
		mod:          ir.NewModule(),
		errorHandler: errorHandler,
		result: &Result{
			Dependencies: make(map[string]struct{}),
			Output:       "",
		},
		cbb:          nil,
		cf:           nil,
		scp:          newScope(nil), // global scope
		cfscp:        nil,
		functions:    map[string]*funcWrapper{},
		latestReturn: nil,
	}
}

// compile the AST contained in c
// if w is not nil, the resulting llir is written to w
// otherwise a string representation is returned in result
func (c *Compiler) Compile(w io.Writer) (result *Result, rerr error) {
	// catch panics and instead set the returned error
	defer func() {
		if err := recover(); err != nil {
			rerr = fmt.Errorf("%v", err)
			if result != nil {
				result.Output = ""
			}
		}
	}()

	// the ast must be valid (and should have been resolved and typechecked beforehand)
	if c.ast.Faulty {
		return nil, fmt.Errorf("Fehlerhafter Syntax Baum")
	}

	c.mod.SourceFilename = c.ast.File // set the module filename (optional metadata)
	c.setupRuntimeFunctions()         // setup internal functions to interact with the ddp-c-runtime
	// called from the ddp-c-runtime after initialization
	ddpmain := c.insertFunction(
		"_ddp_ddpmain",
		nil,
		c.mod.NewFunc("_ddp_ddpmain", ddpint),
	)
	c.cf = ddpmain               // first function is ddpmain
	c.cbb = ddpmain.NewBlock("") // first block

	// visit every statement in the AST and compile it
	for _, stmt := range c.ast.Statements {
		c.visitNode(stmt)
	}

	c.scp = c.exitScope(c.scp) // exit the main scope

	// on success ddpmain returns 0
	c.cbb.NewRet(newInt(0))
	if w != nil {
		_, err := c.mod.WriteTo(w)
		return c.result, err
	} else {
		c.result.Output = c.mod.String()
		return c.result, nil // return the module as string
	}
}

// helper that might be extended later
// it is not intended for the end user to see these errors, as they are compiler bugs
// the errors in the ddp-code were already reported by the parser/typechecker/resolver
func err(msg string, args ...any) {
	// retreive the file and line on which the error occured
	_, file, line, _ := runtime.Caller(1)
	panic(fmt.Errorf("%s, %d: %s", filepath.Base(file), line, fmt.Sprintf(msg, args...)))
}

// if the llvm-ir should be commented
// increases the intermediate file size
var Comments_Enabled = true

func (c *Compiler) commentNode(block *ir.Block, node ast.Node, details string) {
	if Comments_Enabled {
		comment := fmt.Sprintf("F %s, %d:%d: %s", node.Token().File, node.Token().Line(), node.Token().Column(), node)
		if details != "" {
			comment += " (" + details + ")"
		}
		block.Insts = append(block.Insts, irutil.NewComment(comment))
	}
}

func (c *Compiler) comment(comment string, block *ir.Block) {
	if Comments_Enabled {
		block.Insts = append(block.Insts, irutil.NewComment(comment))
	}
}

// helper to visit a single node
func (c *Compiler) visitNode(node ast.Node) {
	node.Accept(c)
}

// helper to evalueate an expression and return its ir value
// if the result is refCounted it's refcount is usually 1
func (c *Compiler) evaluate(expr ast.Expression) value.Value {
	c.visitNode(expr)
	return c.latestReturn
}

// helper to insert a function into the global function map
// returns the ir function
func (c *Compiler) insertFunction(name string, funcDecl *ast.FuncDecl, irFunc *ir.Func) *ir.Func {
	c.functions[name] = &funcWrapper{
		funcDecl: funcDecl,
		irFunc:   irFunc,
	}
	return irFunc
}

// helper to declare a inbuilt function
// sets the linkage to external, callingconvention to ccc
// and inserts the function into the compiler
func (c *Compiler) declareInbuiltFunction(name string, retType types.Type, params ...*ir.Param) {
	fun := c.mod.NewFunc(name, retType, params...)
	fun.CallingConv = enum.CallingConvC
	fun.Linkage = enum.LinkageExternal
	c.insertFunction(name, nil, fun)
}

// declares functions defined in the ddp-c-runtime
func (c *Compiler) setupRuntimeFunctions() {
	c.setupStringType()
	c.setupListTypes()
	c.declareInbuiltFunction("out_of_bounds", void, ir.NewParam("index", i64), ir.NewParam("len", i64)) // helper function for out-of-bounds error
	c.setupOperators()
}

// declares some internal string functions
// and completes the ddpstring struct
func (c *Compiler) setupStringType() {
	// complete the ddpstring definition to interact with the c ddp runtime
	ddpstring.Fields = make([]types.Type, 2)
	ddpstring.Fields[0] = ptr(i8)
	ddpstring.Fields[1] = ddpint
	c.mod.NewTypeDef("ddpstring", ddpstring)

	// declare all the external functions to work with strings

	// creates a ddpstring from a string literal and returns a pointer to it
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_string_from_constant", ddpstrptr, ir.NewParam("str", ptr(i8)))

	// frees the given string
	c.declareInbuiltFunction("_ddp_free_string", void, ir.NewParam("str", ddpstrptr))

	// returns a copy of the passed string as a new pointer
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_deep_copy_string", ddpstrptr, ir.NewParam("str", ddpstrptr))
}

// declares some internal list functions
// and completes the ddp<type>list structs
func (c *Compiler) setupListTypes() {

	// complete the ddpintlist definition to interact with the c ddp runtime
	ddpintlist.Fields = make([]types.Type, 3)
	ddpintlist.Fields[0] = ptr(ddpint)
	ddpintlist.Fields[1] = ddpint
	ddpintlist.Fields[2] = ddpint
	c.mod.NewTypeDef("ddpintlist", ddpintlist)

	// creates a ddpintlist from the elements and returns a pointer to it
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_ddpintlist_from_constants", ddpintlistptr, ir.NewParam("count", ddpint))

	// frees the given list
	c.declareInbuiltFunction("_ddp_free_ddpintlist", void, ir.NewParam("list", ddpintlistptr))

	// returns a copy of the passed string as a new pointer
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_deep_copy_ddpintlist", ddpintlistptr, ir.NewParam("list", ddpintlistptr))

	// inbuilt operators for lists
	c.declareInbuiltFunction("_ddp_ddpintlist_equal", ddpbool, ir.NewParam("list1", ddpintlistptr), ir.NewParam("list2", ddpintlistptr))
	c.declareInbuiltFunction("_ddp_ddpintlist_slice", ddpintlistptr, ir.NewParam("list", ddpintlistptr), ir.NewParam("index1", ddpint), ir.NewParam("index2", ddpint))
	c.declareInbuiltFunction("_ddp_ddpintlist_to_string", ddpstrptr, ir.NewParam("list", ddpintlistptr))

	c.declareInbuiltFunction("_ddp_ddpintlist_ddpintlist_verkettet", ddpintlistptr, ir.NewParam("list1", ddpintlistptr), ir.NewParam("list2", ddpintlistptr))
	c.declareInbuiltFunction("_ddp_ddpintlist_ddpint_verkettet", ddpintlistptr, ir.NewParam("list", ddpintlistptr), ir.NewParam("el", ddpint))

	c.declareInbuiltFunction("_ddp_ddpint_ddpint_verkettet", ddpintlistptr, ir.NewParam("el1", ddpint), ir.NewParam("el2", ddpint))
	c.declareInbuiltFunction("_ddp_ddpint_ddpintlist_verkettet", ddpintlistptr, ir.NewParam("el", ddpint), ir.NewParam("list", ddpintlistptr))

	// complete the ddpfloatlist definition to interact with the c ddp runtime
	ddpfloatlist.Fields = make([]types.Type, 3)
	ddpfloatlist.Fields[0] = ptr(ddpfloat)
	ddpfloatlist.Fields[1] = ddpint
	ddpfloatlist.Fields[2] = ddpint
	c.mod.NewTypeDef("ddpfloatlist", ddpfloatlist)

	// creates a ddpfloatlist from the elements and returns a pointer to it
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_ddpfloatlist_from_constants", ddpfloatlistptr, ir.NewParam("count", ddpint))

	// frees the given list
	c.declareInbuiltFunction("_ddp_free_ddpfloatlist", void, ir.NewParam("list", ddpfloatlistptr))

	// returns a copy of the passed string as a new pointer
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_deep_copy_ddpfloatlist", ddpfloatlistptr, ir.NewParam("list", ddpfloatlistptr))

	// inbuilt operators for lists
	c.declareInbuiltFunction("_ddp_ddpfloatlist_equal", ddpbool, ir.NewParam("list1", ddpfloatlistptr), ir.NewParam("list2", ddpfloatlistptr))
	c.declareInbuiltFunction("_ddp_ddpfloatlist_slice", ddpfloatlistptr, ir.NewParam("list", ddpfloatlistptr), ir.NewParam("index1", ddpint), ir.NewParam("index2", ddpint))
	c.declareInbuiltFunction("_ddp_ddpfloatlist_to_string", ddpstrptr, ir.NewParam("list", ddpfloatlistptr))

	c.declareInbuiltFunction("_ddp_ddpfloatlist_ddpfloatlist_verkettet", ddpfloatlistptr, ir.NewParam("list1", ddpfloatlistptr), ir.NewParam("list2", ddpfloatlistptr))
	c.declareInbuiltFunction("_ddp_ddpfloatlist_ddpfloat_verkettet", ddpfloatlistptr, ir.NewParam("list", ddpfloatlistptr), ir.NewParam("el", ddpfloat))

	c.declareInbuiltFunction("_ddp_ddpfloat_ddpfloat_verkettet", ddpfloatlistptr, ir.NewParam("el1", ddpfloat), ir.NewParam("el2", ddpfloat))
	c.declareInbuiltFunction("_ddp_ddpfloat_ddpfloatlist_verkettet", ddpfloatlistptr, ir.NewParam("el", ddpfloat), ir.NewParam("list", ddpfloatlistptr))

	// complete the ddpboollist definition to interact with the c ddp runtime
	ddpboollist.Fields = make([]types.Type, 3)
	ddpboollist.Fields[0] = ptr(ddpbool)
	ddpboollist.Fields[1] = ddpint
	ddpboollist.Fields[2] = ddpint
	c.mod.NewTypeDef("ddpboollist", ddpboollist)

	// creates a ddpboollist from the elements and returns a pointer to it
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_ddpboollist_from_constants", ddpboollistptr, ir.NewParam("count", ddpint))

	// frees the given list
	c.declareInbuiltFunction("_ddp_free_ddpboollist", void, ir.NewParam("list", ddpboollistptr))

	// returns a copy of the passed string as a new pointer
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_deep_copy_ddpboollist", ddpboollistptr, ir.NewParam("list", ddpboollistptr))

	// inbuilt operators for lists
	c.declareInbuiltFunction("_ddp_ddpboollist_equal", ddpbool, ir.NewParam("list1", ddpboollistptr), ir.NewParam("list2", ddpboollistptr))
	c.declareInbuiltFunction("_ddp_ddpboollist_slice", ddpboollistptr, ir.NewParam("list", ddpboollistptr), ir.NewParam("index1", ddpint), ir.NewParam("index2", ddpint))
	c.declareInbuiltFunction("_ddp_ddpboollist_to_string", ddpstrptr, ir.NewParam("list", ddpboollistptr))

	c.declareInbuiltFunction("_ddp_ddpboollist_ddpboollist_verkettet", ddpboollistptr, ir.NewParam("list1", ddpboollistptr), ir.NewParam("list2", ddpboollistptr))
	c.declareInbuiltFunction("_ddp_ddpboollist_ddpbool_verkettet", ddpboollistptr, ir.NewParam("list", ddpboollistptr), ir.NewParam("el", ddpbool))

	c.declareInbuiltFunction("_ddp_ddpbool_ddpbool_verkettet", ddpboollistptr, ir.NewParam("el1", ddpbool), ir.NewParam("el2", ddpbool))
	c.declareInbuiltFunction("_ddp_ddpbool_ddpboollist_verkettet", ddpboollistptr, ir.NewParam("el", ddpbool), ir.NewParam("list", ddpboollistptr))

	// complete the ddpcharlist definition to interact with the c ddp runtime
	ddpcharlist.Fields = make([]types.Type, 3)
	ddpcharlist.Fields[0] = ptr(ddpchar)
	ddpcharlist.Fields[1] = ddpint
	ddpcharlist.Fields[2] = ddpint
	c.mod.NewTypeDef("ddpcharlist", ddpcharlist)

	// creates a ddpcharlist from the elements and returns a pointer to it
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_ddpcharlist_from_constants", ddpcharlistptr, ir.NewParam("count", ddpint))

	// frees the given list
	c.declareInbuiltFunction("_ddp_free_ddpcharlist", void, ir.NewParam("list", ddpcharlistptr))

	// returns a copy of the passed string as a new pointer
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_deep_copy_ddpcharlist", ddpcharlistptr, ir.NewParam("list", ddpcharlistptr))

	// inbuilt operators for lists
	c.declareInbuiltFunction("_ddp_ddpcharlist_equal", ddpbool, ir.NewParam("list1", ddpcharlistptr), ir.NewParam("list2", ddpcharlistptr))
	c.declareInbuiltFunction("_ddp_ddpcharlist_slice", ddpcharlistptr, ir.NewParam("list", ddpcharlistptr), ir.NewParam("index1", ddpint), ir.NewParam("index2", ddpint))
	c.declareInbuiltFunction("_ddp_ddpcharlist_to_string", ddpstrptr, ir.NewParam("list", ddpcharlistptr))

	c.declareInbuiltFunction("_ddp_ddpcharlist_ddpcharlist_verkettet", ddpcharlistptr, ir.NewParam("list1", ddpcharlistptr), ir.NewParam("list2", ddpcharlistptr))
	c.declareInbuiltFunction("_ddp_ddpcharlist_ddpchar_verkettet", ddpcharlistptr, ir.NewParam("list", ddpcharlistptr), ir.NewParam("el", ddpchar))

	c.declareInbuiltFunction("_ddp_ddpchar_ddpchar_verkettet", ddpcharlistptr, ir.NewParam("el1", ddpchar), ir.NewParam("el2", ddpchar))
	c.declareInbuiltFunction("_ddp_ddpchar_ddpcharlist_verkettet", ddpcharlistptr, ir.NewParam("el", ddpchar), ir.NewParam("list", ddpcharlistptr))

	// complete the ddpstringlist definition to interact with the c ddp runtime
	ddpstringlist.Fields = make([]types.Type, 3)
	ddpstringlist.Fields[0] = ptr(ddpstrptr)
	ddpstringlist.Fields[1] = ddpint
	ddpstringlist.Fields[2] = ddpint
	c.mod.NewTypeDef("ddpstringlist", ddpstringlist)

	// creates a ddpstringlist from the elements and returns a pointer to it
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_ddpstringlist_from_constants", ddpstringlistptr, ir.NewParam("count", ddpint))

	// frees the given list
	c.declareInbuiltFunction("_ddp_free_ddpstringlist", void, ir.NewParam("list", ddpstringlistptr))

	// returns a copy of the passed string as a new pointer
	// the caller is responsible for calling increment_ref_count on this pointer
	c.declareInbuiltFunction("_ddp_deep_copy_ddpstringlist", ddpstringlistptr, ir.NewParam("list", ddpstringlistptr))

	// inbuilt operators for lists
	c.declareInbuiltFunction("_ddp_ddpstringlist_equal", ddpbool, ir.NewParam("list1", ddpstringlistptr), ir.NewParam("list2", ddpstringlistptr))
	c.declareInbuiltFunction("_ddp_ddpstringlist_slice", ddpstringlistptr, ir.NewParam("list", ddpstringlistptr), ir.NewParam("index1", ddpint), ir.NewParam("index2", ddpint))
	c.declareInbuiltFunction("_ddp_ddpstringlist_to_string", ddpstrptr, ir.NewParam("list", ddpstringlistptr))

	c.declareInbuiltFunction("_ddp_ddpstringlist_ddpstringlist_verkettet", ddpstringlistptr, ir.NewParam("list1", ddpstringlistptr), ir.NewParam("list2", ddpstringlistptr))
	c.declareInbuiltFunction("_ddp_ddpstringlist_ddpstring_verkettet", ddpstringlistptr, ir.NewParam("list", ddpstringlistptr), ir.NewParam("el", ddpstrptr))

	c.declareInbuiltFunction("_ddp_ddpstring_ddpstringlist_verkettet", ddpstringlistptr, ir.NewParam("str", ddpstrptr), ir.NewParam("list", ddpstringlistptr))
}

func (c *Compiler) setupOperators() {
	// betrag operator for different types
	c.declareInbuiltFunction("llabs", ddpint, ir.NewParam("i", ddpint))
	c.declareInbuiltFunction("fabs", ddpfloat, ir.NewParam("f", ddpfloat))

	// hoch operator for different type combinations
	c.declareInbuiltFunction("pow", ddpfloat, ir.NewParam("f1", ddpfloat), ir.NewParam("f2", ddpfloat))

	// trigonometric functions
	c.declareInbuiltFunction("_ddp_sin", ddpfloat, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_cos", ddpfloat, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_tan", ddpfloat, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_asin", ddpfloat, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_acos", ddpfloat, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_atan", ddpfloat, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_sinh", ddpfloat, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_cosh", ddpfloat, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_tanh", ddpfloat, ir.NewParam("f", ddpfloat))

	// logarithm
	c.declareInbuiltFunction("log10", ddpfloat, ir.NewParam("f", ddpfloat))

	// ddpstring length
	c.declareInbuiltFunction("_ddp_string_length", ddpint, ir.NewParam("str", ddpstrptr))

	// ddpstring to type cast
	c.declareInbuiltFunction("_ddp_string_to_int", ddpint, ir.NewParam("str", ddpstrptr))
	c.declareInbuiltFunction("_ddp_string_to_float", ddpfloat, ir.NewParam("str", ddpstrptr))

	// casts to ddpstring
	c.declareInbuiltFunction("_ddp_int_to_string", ddpstrptr, ir.NewParam("i", ddpint))
	c.declareInbuiltFunction("_ddp_float_to_string", ddpstrptr, ir.NewParam("f", ddpfloat))
	c.declareInbuiltFunction("_ddp_bool_to_string", ddpstrptr, ir.NewParam("b", ddpbool))
	c.declareInbuiltFunction("_ddp_char_to_string", ddpstrptr, ir.NewParam("c", ddpchar))

	// ddpstring equality
	c.declareInbuiltFunction("_ddp_string_equal", ddpbool, ir.NewParam("str1", ddpstrptr), ir.NewParam("str2", ddpstrptr))

	// ddpstring concatenation for different types
	c.declareInbuiltFunction("_ddp_string_string_verkettet", ddpstrptr, ir.NewParam("str1", ddpstrptr), ir.NewParam("str2", ddpstrptr))
	c.declareInbuiltFunction("_ddp_char_string_verkettet", ddpstrptr, ir.NewParam("c", ddpchar), ir.NewParam("str", ddpstrptr))
	c.declareInbuiltFunction("_ddp_string_char_verkettet", ddpstrptr, ir.NewParam("str", ddpstrptr), ir.NewParam("c", ddpchar))
	c.declareInbuiltFunction("_ddp_char_char_verkettet", ddpstrptr, ir.NewParam("c1", ddpchar), ir.NewParam("c2", ddpchar))

	// string indexing
	c.declareInbuiltFunction("_ddp_string_index", ddpchar, ir.NewParam("str", ddpstrptr), ir.NewParam("index", ddpint))
	c.declareInbuiltFunction("_ddp_replace_char_in_string", void, ir.NewParam("str", ddpstrptr), ir.NewParam("ch", ddpchar), ir.NewParam("index", ddpint))
	c.declareInbuiltFunction("_ddp_string_slice", ddpstrptr, ir.NewParam("str", ddpstrptr), ir.NewParam("index1", ddpint), ir.NewParam("index2", ddpint))
}

// helper to call _ddp_free_<type>
// which is a dynamically allocated type
func (c *Compiler) freeDynamic(value_ptr value.Value) {
	switch value_ptr.Type() {
	case ddpstrptr:
		c.cbb.NewCall(c.functions["_ddp_free_string"].irFunc, value_ptr)
	case ddpintlistptr:
		c.cbb.NewCall(c.functions["_ddp_free_ddpintlist"].irFunc, value_ptr)
	case ddpfloatlistptr:
		c.cbb.NewCall(c.functions["_ddp_free_ddpfloatlist"].irFunc, value_ptr)
	case ddpboollistptr:
		c.cbb.NewCall(c.functions["_ddp_free_ddpboollist"].irFunc, value_ptr)
	case ddpcharlistptr:
		c.cbb.NewCall(c.functions["_ddp_free_ddpcharlist"].irFunc, value_ptr)
	case ddpstringlistptr:
		c.cbb.NewCall(c.functions["_ddp_free_ddpstringlist"].irFunc, value_ptr)
	default:
		err("invalid type %s", value_ptr)
	}
}

// helper to call _ddp_deep_copy_<type>
// which is a dynamically allocated type
func (c *Compiler) deepCopyDynamic(value_ptr value.Value) value.Value {
	switch value_ptr.Type() {
	case ddpstrptr:
		return c.cbb.NewCall(c.functions["_ddp_deep_copy_string"].irFunc, value_ptr)
	case ddpintlistptr:
		return c.cbb.NewCall(c.functions["_ddp_deep_copy_ddpintlist"].irFunc, value_ptr)
	case ddpfloatlistptr:
		return c.cbb.NewCall(c.functions["_ddp_deep_copy_ddpfloatlist"].irFunc, value_ptr)
	case ddpboollistptr:
		return c.cbb.NewCall(c.functions["_ddp_deep_copy_ddpboollist"].irFunc, value_ptr)
	case ddpcharlistptr:
		return c.cbb.NewCall(c.functions["_ddp_deep_copy_ddpcharlist"].irFunc, value_ptr)
	case ddpstringlistptr:
		return c.cbb.NewCall(c.functions["_ddp_deep_copy_ddpstringlist"].irFunc, value_ptr)
	}
	err("invalid type %s", value_ptr)
	return zero // unreachable
}

// helper to exit a scope
// decrements the ref-count on all local variables
// returns the enclosing scope
func (c *Compiler) exitScope(scp *scope) *scope {
	for _, v := range scp.variables {
		if isDynamic(v.typ) && !v.isRef {
			c.freeDynamic(c.cbb.NewLoad(v.typ, v.val))
		}
	}
	return scp.enclosing
}

func (*Compiler) BaseVisitor() {}

// should have been filtered by the resolver/typechecker, so err
func (c *Compiler) VisitBadDecl(d *ast.BadDecl) {
	err("Es wurde eine invalide Deklaration gefunden")
}
func (c *Compiler) VisitVarDecl(d *ast.VarDecl) {
	Typ := toIRType(d.Type) // get the llvm type
	// allocate the variable on the function call frame
	// all local variables are allocated in the first basic block of the function they are within
	// in the ir a local variable is a alloca instruction (a stack allocation)
	// global variables are allocated in the ddpmain function
	c.commentNode(c.cbb, d, d.Name.Literal)
	var varLocation value.Value
	if c.scp.enclosing == nil { // global scope
		varLocation = c.mod.NewGlobalDef("", getDefaultValue(d.Type))
	} else {
		varLocation = c.cf.Blocks[0].NewAlloca(Typ)
	}
	Var := c.scp.addVar(d.Name.Literal, varLocation, Typ, false)
	initVal := c.evaluate(d.InitVal) // evaluate the initial value
	c.cbb.NewStore(initVal, Var)     // store the initial value
}
func (c *Compiler) VisitFuncDecl(d *ast.FuncDecl) {
	retType := toIRType(d.Type)                       // get the llvm type
	params := make([]*ir.Param, 0, len(d.ParamTypes)) // list of the ir parameters

	// append all the other parameters
	for i, typ := range d.ParamTypes {
		ty := toIRTypeRef(typ)                                            // convert the type of the parameter
		params = append(params, ir.NewParam(d.ParamNames[i].Literal, ty)) // add it to the list
	}

	irFunc := c.mod.NewFunc(d.Name.Literal, retType, params...) // create the ir function
	irFunc.CallingConv = enum.CallingConvC                      // every function is called with the c calling convention to make interaction with inbuilt stuff easier

	c.insertFunction(d.Name.Literal, d, irFunc)

	// inbuilt or external functions are defined in c
	if ast.IsExternFunc(d) {
		irFunc.Linkage = enum.LinkageExternal
		path, err := filepath.Abs(filepath.Join(filepath.Dir(d.Token().File), strings.Trim(d.ExternFile.Literal, "\"")))
		if err != nil {
			c.errorHandler(&CompilerError{file: d.ExternFile.File, rang: d.ExternFile.Range, msg: err.Error()})
		}
		c.result.Dependencies[path] = struct{}{} // add the file-path where the function is defined to the dependencies set
	} else {
		fun, block := c.cf, c.cbb // safe the state before the function body
		c.cf, c.cbb, c.scp = irFunc, irFunc.NewBlock(""), newScope(c.scp)
		c.cfscp = c.scp
		// passed arguments are immutible (llvm uses ssa registers) so we declare them as local variables
		// the caller of the function is responsible for managing the ref-count of garbage collected values
		for i := range params {
			irType := toIRType(d.ParamTypes[i].Type)
			if d.ParamTypes[i].IsReference {
				// references are implemented similar to name-shadowing
				// they basically just get another name in the function scope, which
				// refers to the same variable allocation
				c.scp.addVar(params[i].LocalIdent.Name(), params[i], irType, true)
			} else if isDynamic(irType) { // strings and lists need special handling
				// add the local variable for the parameter
				v := c.scp.addVar(params[i].LocalIdent.Name(), c.cbb.NewAlloca(irType), irType, false)
				c.cbb.NewStore(params[i], v) // store the copy in the local variable
			} else {
				// non garbage-collected types are just declared as their ir type
				v := c.scp.addVar(params[i].LocalIdent.Name(), c.cbb.NewAlloca(irType), irType, false)
				c.cbb.NewStore(params[i], v)
			}
		}

		// modified VisitBlockStmt
		c.scp = newScope(c.scp) // a block gets its own scope
		toplevelReturn := false
		for _, stmt := range d.Body.Statements {
			c.visitNode(stmt)
			// on toplevel return statements, ignore anything that follows
			if _, ok := stmt.(*ast.ReturnStmt); ok {
				toplevelReturn = true
				break
			}
		}
		// free the local variables of the function
		if toplevelReturn {
			c.scp = c.scp.enclosing
		} else {
			c.scp = c.exitScope(c.scp)
		}

		if c.cbb.Term == nil {
			c.cbb.NewRet(nil) // every block needs a terminator, and every function a return
		}

		// free the parameters of the function
		if toplevelReturn {
			c.scp = c.scp.enclosing
		} else {
			c.scp = c.exitScope(c.scp)
		}
		c.cf, c.cbb, c.cfscp = fun, block, nil // restore state before the function (to main)
	}
}

// should have been filtered by the resolver/typechecker, so err
func (c *Compiler) VisitBadExpr(e *ast.BadExpr) {
	err("Es wurde ein invalider Ausdruck gefunden")
}
func (c *Compiler) VisitIdent(e *ast.Ident) {
	Var := c.scp.lookupVar(e.Literal.Literal) // get the alloca in the ir
	c.commentNode(c.cbb, e, e.Literal.Literal)
	if isDynamic(Var.typ) { // strings must be copied in case the user of the expression modifies them
		c.latestReturn = c.deepCopyDynamic(c.cbb.NewLoad(Var.typ, Var.val))
	} else { // other variables are simply copied
		c.latestReturn = c.cbb.NewLoad(Var.typ, Var.val)
	}
}

// helper for list indexing
// takes the list and index values as parameters + a Node for comments
// and returns a pointer to the element
func (c *Compiler) getElementPointer(lhs, rhs value.Value, node ast.Node) *ir.InstGetElementPtr {
	thenBlock, errorBlock := c.cf.NewBlock(""), c.cf.NewBlock("")
	// get the length of the list
	lenptr := c.cbb.NewGetElementPtr(derefListPtr(lhs.Type()), lhs, newIntT(i32, 0), newIntT(i32, 1))
	len := c.cbb.NewLoad(ddpint, lenptr)
	// get the 0 based index
	index := c.cbb.NewSub(rhs, newInt(1))
	// bounds check
	cond := c.cbb.NewAnd(c.cbb.NewICmp(enum.IPredSLT, index, len), c.cbb.NewICmp(enum.IPredSGE, index, newInt(0)))
	c.commentNode(c.cbb, node, "")
	c.cbb.NewCondBr(cond, thenBlock, errorBlock)

	// out of bounds error
	c.cbb = errorBlock
	c.cbb.NewCall(c.functions["out_of_bounds"].irFunc, rhs, len)
	c.commentNode(c.cbb, node, "")
	c.cbb.NewUnreachable()

	c.cbb = thenBlock
	// get a pointer to the array
	arrptr := c.cbb.NewGetElementPtr(derefListPtr(lhs.Type()), lhs, newIntT(i32, 0), newIntT(i32, 0))
	// get the array
	arr := c.cbb.NewLoad(ptr(getElementType(lhs.Type())), arrptr)
	// index into the array
	return c.cbb.NewGetElementPtr(getElementType(lhs.Type()), arr, index)
}

func (c *Compiler) VisitIndexing(e *ast.Indexing) {
	lhs := c.evaluate(e.Lhs)   // TODO: check if this works
	rhs := c.evaluate(e.Index) // rhs is never refCounted
	switch lhs.Type() {
	case ddpstrptr:
		c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_index"].irFunc, lhs, rhs)
	case ddpintlistptr, ddpfloatlistptr, ddpboollistptr, ddpcharlistptr, ddpstringlistptr:
		elementPtr := c.getElementPointer(lhs, rhs, e)
		// load the element
		c.latestReturn = c.cbb.NewLoad(getElementType(lhs.Type()), elementPtr)
		// copy strings
		if lhs.Type() == ddpstringlistptr {
			c.latestReturn = c.deepCopyDynamic(c.latestReturn)
		}
	default:
		err("invalid Parameter Types for STELLE (%s, %s)", lhs.Type(), rhs.Type())
	}
	if isDynamic(lhs.Type()) {
		c.freeDynamic(lhs)
	}
}

// literals are simple ir constants
func (c *Compiler) VisitIntLit(e *ast.IntLit) {
	c.commentNode(c.cbb, e, "")
	c.latestReturn = newInt(e.Value)
}
func (c *Compiler) VisitFloatLit(e *ast.FloatLit) {
	c.commentNode(c.cbb, e, "")
	c.latestReturn = constant.NewFloat(ddpfloat, e.Value)
}
func (c *Compiler) VisitBoolLit(e *ast.BoolLit) {
	c.commentNode(c.cbb, e, "")
	c.latestReturn = constant.NewBool(e.Value)
}
func (c *Compiler) VisitCharLit(e *ast.CharLit) {
	c.commentNode(c.cbb, e, "")
	c.latestReturn = newIntT(ddpchar, int64(e.Value))
}

// string literals are created by the ddp-c-runtime
// so we need to do some work here
func (c *Compiler) VisitStringLit(e *ast.StringLit) {
	constStr := c.mod.NewGlobalDef("", irutil.NewCString(e.Value))
	// call the ddp-runtime function to create the ddpstring
	c.commentNode(c.cbb, e, constStr.Name())
	c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_from_constant"].irFunc, c.cbb.NewBitCast(constStr, ptr(i8)))
}
func (c *Compiler) VisitListLit(e *ast.ListLit) {
	if e.Values != nil {
		list := c.cbb.NewCall(c.functions["_ddp_"+getTypeName(e.Type)+"_from_constants"].irFunc, newInt(int64(len(e.Values))))
		for i, v := range e.Values {
			val := c.evaluate(v)
			arrptr := c.cbb.NewGetElementPtr(derefListPtr(list.Type()), list, newIntT(i32, 0), newIntT(i32, 0))
			arr := c.cbb.NewLoad(ptr(getElementType(list.Type())), arrptr)
			elementPtr := c.cbb.NewGetElementPtr(getElementType(list.Type()), arr, newInt(int64(i)))
			c.cbb.NewStore(val, elementPtr)
		}
		c.latestReturn = list
	} else if e.Count != nil && e.Value != nil {
		count := c.evaluate(e.Count)
		Value := c.evaluate(e.Value)

		list := c.cbb.NewCall(c.functions["_ddp_"+getTypeName(e.Type)+"_from_constants"].irFunc, count)

		counter := c.cbb.NewAlloca(ddpint)
		c.cbb.NewStore(zero, counter)

		condBlock, bodyBlock, leaveBlock := c.cf.NewBlock(""), c.cf.NewBlock(""), c.cf.NewBlock("")
		c.commentNode(c.cbb, e, "")
		c.cbb.NewBr(condBlock)

		c.cbb = condBlock
		cond := c.cbb.NewICmp(enum.IPredSLT, c.cbb.NewLoad(ddpint, counter), count)
		c.commentNode(c.cbb, e, "")
		c.cbb.NewCondBr(cond, bodyBlock, leaveBlock)

		c.cbb = bodyBlock
		index := c.cbb.NewLoad(ddpint, counter)
		var val value.Value
		if isDynamic(Value.Type()) {
			val = c.deepCopyDynamic(Value)
		} else {
			val = Value
		}
		arrptr := c.cbb.NewGetElementPtr(derefListPtr(list.Type()), list, newIntT(i32, 0), newIntT(i32, 0))
		arr := c.cbb.NewLoad(ptr(getElementType(list.Type())), arrptr)
		elementPtr := c.cbb.NewGetElementPtr(getElementType(list.Type()), arr, index)
		c.cbb.NewStore(val, elementPtr)
		c.cbb.NewStore(c.cbb.NewAdd(index, newInt(1)), counter)
		c.commentNode(c.cbb, e, "")
		c.cbb.NewBr(condBlock)

		c.cbb = leaveBlock
		if isDynamic(Value.Type()) {
			c.freeDynamic(Value)
		}

		c.latestReturn = list
	} else {
		c.latestReturn = c.cbb.NewCall(c.functions["_ddp_"+getTypeName(e.Type)+"_from_constants"].irFunc, zero)
	}
}
func (c *Compiler) VisitUnaryExpr(e *ast.UnaryExpr) {
	rhs := c.evaluate(e.Rhs) // compile the expression onto which the operator is applied
	// big switches for the different type combinations
	c.commentNode(c.cbb, e, e.Operator.String())
	switch e.Operator.Type {
	case token.BETRAG:
		switch rhs.Type() {
		case ddpfloat:
			c.latestReturn = c.cbb.NewCall(c.functions["fabs"].irFunc, rhs)
		case ddpint:
			c.latestReturn = c.cbb.NewCall(c.functions["llabs"].irFunc, rhs)
		default:
			err("invalid Parameter Type for BETRAG: %s", rhs.Type())
		}
	case token.NEGATE:
		switch rhs.Type() {
		case ddpfloat:
			c.latestReturn = c.cbb.NewFNeg(rhs)
		case ddpint:
			c.latestReturn = c.cbb.NewSub(zero, rhs)
		default:
			err("invalid Parameter Type for NEGATE: %s", rhs.Type())
		}
	case token.NICHT:
		c.latestReturn = c.cbb.NewXor(rhs, newInt(1))
	case token.NEGIERE:
		switch rhs.Type() {
		case ddpbool:
			c.latestReturn = c.cbb.NewXor(rhs, newInt(1))
		case ddpint:
			c.latestReturn = c.cbb.NewXor(rhs, newInt(all_ones))
		}
	case token.LOGISCHNICHT:
		c.latestReturn = c.cbb.NewXor(rhs, newInt(all_ones))
	case token.LÄNGE:
		switch rhs.Type() {
		case ddpstrptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_length"].irFunc, rhs)
		case ddpintlistptr, ddpfloatlistptr, ddpboollistptr, ddpcharlistptr, ddpstringlistptr:
			lenptr := c.cbb.NewGetElementPtr(derefListPtr(rhs.Type()), rhs, newIntT(i32, 0), newIntT(i32, 1))
			c.latestReturn = c.cbb.NewLoad(ddpint, lenptr)
		default:
			err("invalid Parameter Type for LÄNGE: %s", rhs.Type())
		}
	case token.GRÖßE:
		switch rhs.Type() {
		case ddpint, ddpfloat:
			c.latestReturn = newInt(8)
		case ddpbool:
			c.latestReturn = newInt(1)
		case ddpchar:
			c.latestReturn = newInt(4)
		case ddpstrptr:
			strcapptr := c.cbb.NewGetElementPtr(ddpstring, rhs, newIntT(i32, 0), newIntT(i32, 1))
			strcap := c.cbb.NewLoad(ddpint, strcapptr)
			c.latestReturn = c.cbb.NewAdd(strcap, newInt(16))
		case ddpintlistptr, ddpfloatlistptr, ddpboollistptr, ddpcharlistptr, ddpstringlistptr:
			c.latestReturn = newInt(24) // TODO: this
		default:
			err("invalid Parameter Type for GRÖßE: %s", rhs.Type())
		}
	default:
		err("Unbekannter Operator '%s'", e.Operator)
	}
	if isDynamic(rhs.Type()) {
		c.freeDynamic(rhs)
	}
}
func (c *Compiler) VisitBinaryExpr(e *ast.BinaryExpr) {
	// for UND and ODER both operands are booleans, so no refcounting needs to be done
	switch e.Operator.Type {
	case token.UND:
		lhs := c.evaluate(e.Lhs)
		startBlock, trueBlock, leaveBlock := c.cbb, c.cf.NewBlock(""), c.cf.NewBlock("")
		c.commentNode(c.cbb, e, e.Operator.String())
		c.cbb.NewCondBr(lhs, trueBlock, leaveBlock)

		c.cbb = trueBlock
		rhs := c.evaluate(e.Rhs)
		c.commentNode(c.cbb, e, e.Operator.String())
		c.cbb.NewBr(leaveBlock)
		trueBlock = c.cbb

		c.cbb = leaveBlock
		c.commentNode(c.cbb, e, e.Operator.String())
		c.latestReturn = c.cbb.NewPhi(ir.NewIncoming(rhs, trueBlock), ir.NewIncoming(lhs, startBlock))
		return
	case token.ODER:
		lhs := c.evaluate(e.Lhs)
		startBlock, falseBlock, leaveBlock := c.cbb, c.cf.NewBlock(""), c.cf.NewBlock("")
		c.commentNode(c.cbb, e, e.Operator.String())
		c.cbb.NewCondBr(lhs, leaveBlock, falseBlock)

		c.cbb = falseBlock
		rhs := c.evaluate(e.Rhs)
		c.commentNode(c.cbb, e, e.Operator.String())
		c.cbb.NewBr(leaveBlock)
		falseBlock = c.cbb // incase c.evaluate has multiple blocks

		c.cbb = leaveBlock
		c.commentNode(c.cbb, e, e.Operator.String())
		c.latestReturn = c.cbb.NewPhi(ir.NewIncoming(lhs, startBlock), ir.NewIncoming(rhs, falseBlock))
		return
	}

	// compile the two expressions onto which the operator is applied
	lhs := c.evaluate(e.Lhs)
	rhs := c.evaluate(e.Rhs)
	// big switches on the different type combinations
	c.commentNode(c.cbb, e, e.Operator.String())
	switch e.Operator.Type {
	case token.VERKETTET:
		switch lhs.Type() {
		case ddpintlistptr:
			switch rhs.Type() {
			case ddpintlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpintlist_ddpintlist_verkettet"].irFunc, lhs, rhs)
			case ddpint:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpintlist_ddpint_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpint_ddpint_verkettet"].irFunc, lhs, rhs)
			case ddpintlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpint_ddpintlist_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloatlistptr:
			switch rhs.Type() {
			case ddpfloatlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpfloatlist_ddpfloatlist_verkettet"].irFunc, lhs, rhs)
			case ddpfloat:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpfloatlist_ddpfloat_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpfloat:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpfloat_ddpfloat_verkettet"].irFunc, lhs, rhs)
			case ddpfloatlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpfloat_ddpfloatlist_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpboollistptr:
			switch rhs.Type() {
			case ddpboollistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpboollist_ddpboollist_verkettet"].irFunc, lhs, rhs)
			case ddpbool:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpboollist_ddpbool_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpbool:
			switch rhs.Type() {
			case ddpbool:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpbool_ddpbool_verkettet"].irFunc, lhs, rhs)
			case ddpboollistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpbool_ddpboollist_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpcharlistptr:
			switch rhs.Type() {
			case ddpcharlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpcharlist_ddpcharlist_verkettet"].irFunc, lhs, rhs)
			case ddpchar:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpcharlist_ddpchar_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpchar:
			switch rhs.Type() {
			case ddpchar:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpchar_ddpchar_verkettet"].irFunc, lhs, rhs)
			case ddpstrptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_char_string_verkettet"].irFunc, lhs, rhs)
			case ddpcharlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpchar_ddpcharlist_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpstringlistptr:
			switch rhs.Type() {
			case ddpstringlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpstringlist_ddpstringlist_verkettet"].irFunc, lhs, rhs)
			case ddpstrptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpstringlist_ddpstring_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpstrptr:
			switch rhs.Type() {
			case ddpstrptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_string_verkettet"].irFunc, lhs, rhs)
			case ddpchar:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_char_verkettet"].irFunc, lhs, rhs)
			case ddpstringlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpstring_ddpstringlist_verkettet"].irFunc, lhs, rhs)
			default:
				err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for VERKETTET (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.PLUS, token.ADDIERE, token.ERHÖHE:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewAdd(lhs, rhs)
			case ddpfloat:
				fp := c.cbb.NewSIToFP(lhs, ddpfloat)
				c.latestReturn = c.cbb.NewFAdd(fp, rhs)
			default:
				err("invalid Parameter Types for PLUS (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				fp := c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFAdd(lhs, fp)
			case ddpfloat:
				c.latestReturn = c.cbb.NewFAdd(lhs, rhs)
			default:
				err("invalid Parameter Types for PLUS (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for PLUS (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.MINUS, token.SUBTRAHIERE, token.VERRINGERE:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewSub(lhs, rhs)
			case ddpfloat:
				fp := c.cbb.NewSIToFP(lhs, ddpfloat)
				c.latestReturn = c.cbb.NewFSub(fp, rhs)
			default:
				err("invalid Parameter Types for MINUS (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				fp := c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFSub(lhs, fp)
			case ddpfloat:
				c.latestReturn = c.cbb.NewFSub(lhs, rhs)
			default:
				err("invalid Parameter Types for MINUS (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for MINUS (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.MAL, token.MULTIPLIZIERE, token.VERVIELFACHE:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewMul(lhs, rhs)
			case ddpfloat:
				fp := c.cbb.NewSIToFP(lhs, ddpfloat)
				c.latestReturn = c.cbb.NewFMul(fp, rhs)
			default:
				err("invalid Parameter Types for MAL (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				fp := c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFMul(lhs, fp)
			case ddpfloat:
				c.latestReturn = c.cbb.NewFMul(lhs, rhs)
			default:
				err("invalid Parameter Types for MAL (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for MAL (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.DURCH, token.DIVIDIERE, token.TEILE:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				lhs = c.cbb.NewSIToFP(lhs, ddpfloat)
				rhs = c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFDiv(lhs, rhs)
			case ddpfloat:
				fp := c.cbb.NewSIToFP(lhs, ddpfloat)
				c.latestReturn = c.cbb.NewFDiv(fp, rhs)
			default:
				err("invalid Parameter Types for DURCH (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				fp := c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFDiv(lhs, fp)
			case ddpfloat:
				c.latestReturn = c.cbb.NewFDiv(lhs, rhs)
			default:
				err("invalid Parameter Types for DURCH (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for DURCH (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.STELLE:
		switch lhs.Type() {
		case ddpstrptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_index"].irFunc, lhs, rhs)
		case ddpintlistptr, ddpfloatlistptr, ddpboollistptr, ddpcharlistptr, ddpstringlistptr:
			thenBlock, errorBlock, leaveBlock := c.cf.NewBlock(""), c.cf.NewBlock(""), c.cf.NewBlock("")
			// get the length of the list
			lenptr := c.cbb.NewGetElementPtr(derefListPtr(lhs.Type()), lhs, newIntT(i32, 0), newIntT(i32, 1))
			len := c.cbb.NewLoad(ddpint, lenptr)
			// get the 0 based index
			index := c.cbb.NewSub(rhs, newInt(1))
			// bounds check
			cond := c.cbb.NewAnd(c.cbb.NewICmp(enum.IPredSLT, index, len), c.cbb.NewICmp(enum.IPredSGE, index, newInt(0)))
			c.commentNode(c.cbb, e, "")
			c.cbb.NewCondBr(cond, thenBlock, errorBlock)

			// out of bounds error
			c.cbb = errorBlock
			c.cbb.NewCall(c.functions["out_of_bounds"].irFunc, rhs, len)
			c.commentNode(c.cbb, e, "")
			c.cbb.NewUnreachable()

			c.cbb = thenBlock
			// get a pointer to the array
			arrptr := c.cbb.NewGetElementPtr(derefListPtr(lhs.Type()), lhs, newIntT(i32, 0), newIntT(i32, 0))
			// get the array
			arr := c.cbb.NewLoad(ptr(getElementType(lhs.Type())), arrptr)
			// index into the array
			elementPtr := c.cbb.NewGetElementPtr(getElementType(lhs.Type()), arr, index)
			// load the element
			c.latestReturn = c.cbb.NewLoad(getElementType(lhs.Type()), elementPtr)
			// copy strings
			if lhs.Type() == ddpstringlistptr {
				c.latestReturn = c.deepCopyDynamic(c.latestReturn)
			}
			c.commentNode(c.cbb, e, "")
			c.cbb.NewBr(leaveBlock)
			c.cbb = leaveBlock
		default:
			err("invalid Parameter Types for STELLE (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.HOCH:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				lhs = c.cbb.NewSIToFP(lhs, ddpfloat)
				rhs = c.cbb.NewSIToFP(rhs, ddpfloat)
			case ddpfloat:
				lhs = c.cbb.NewSIToFP(lhs, ddpfloat)
			default:
				err("invalid Parameter Types for HOCH (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				rhs = c.cbb.NewSIToFP(rhs, ddpfloat)
			default:
				err("invalid Parameter Types for HOCH (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for HOCH (%s, %s)", lhs.Type(), rhs.Type())
		}
		c.latestReturn = c.cbb.NewCall(c.functions["pow"].irFunc, lhs, rhs)
	case token.LOGARITHMUS:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				lhs = c.cbb.NewSIToFP(lhs, ddpfloat)
				rhs = c.cbb.NewSIToFP(rhs, ddpfloat)
			case ddpfloat:
				lhs = c.cbb.NewSIToFP(lhs, ddpfloat)
			default:
				err("invalid Parameter Types for LOGARITHMUS (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				rhs = c.cbb.NewSIToFP(rhs, ddpfloat)
			default:
				err("invalid Parameter Types for LOGARITHMUS (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for LOGARITHMUS (%s, %s)", lhs.Type(), rhs.Type())
		}
		log10_num := c.cbb.NewCall(c.functions["log10"].irFunc, lhs)
		log10_base := c.cbb.NewCall(c.functions["log10"].irFunc, rhs)
		c.latestReturn = c.cbb.NewFDiv(log10_num, log10_base)
	case token.LOGISCHUND:
		c.latestReturn = c.cbb.NewAnd(lhs, rhs)
	case token.LOGISCHODER:
		c.latestReturn = c.cbb.NewOr(lhs, rhs)
	case token.KONTRA:
		c.latestReturn = c.cbb.NewXor(lhs, rhs)
	case token.MODULO:
		c.latestReturn = c.cbb.NewSRem(lhs, rhs)
	case token.LINKS:
		c.latestReturn = c.cbb.NewShl(lhs, rhs)
	case token.RECHTS:
		c.latestReturn = c.cbb.NewLShr(lhs, rhs)
	case token.GLEICH:
		switch lhs.Type() {
		case ddpint:
			c.latestReturn = c.cbb.NewICmp(enum.IPredEQ, lhs, rhs)
		case ddpfloat:
			c.latestReturn = c.cbb.NewFCmp(enum.FPredOEQ, lhs, rhs)
		case ddpbool:
			c.latestReturn = c.cbb.NewICmp(enum.IPredEQ, lhs, rhs)
		case ddpchar:
			c.latestReturn = c.cbb.NewICmp(enum.IPredEQ, lhs, rhs)
		case ddpstrptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_equal"].irFunc, lhs, rhs)
		case ddpintlistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpintlist_equal"].irFunc, lhs, rhs)
		case ddpfloatlistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpfloatlist_equal"].irFunc, lhs, rhs)
		case ddpboollistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpboollist_equal"].irFunc, lhs, rhs)
		case ddpcharlistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpcharlist_equal"].irFunc, lhs, rhs)
		case ddpstringlistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpstringlist_equal"].irFunc, lhs, rhs)
		default:
			err("invalid Parameter Types for GLEICH (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.UNGLEICH:
		switch lhs.Type() {
		case ddpint:
			c.latestReturn = c.cbb.NewICmp(enum.IPredNE, lhs, rhs)
		case ddpfloat:
			c.latestReturn = c.cbb.NewFCmp(enum.FPredONE, lhs, rhs)
		case ddpbool:
			c.latestReturn = c.cbb.NewICmp(enum.IPredNE, lhs, rhs)
		case ddpchar:
			c.latestReturn = c.cbb.NewICmp(enum.IPredNE, lhs, rhs)
		case ddpstrptr:
			equal := c.cbb.NewCall(c.functions["_ddp_string_equal"].irFunc, lhs, rhs)
			c.latestReturn = c.cbb.NewXor(equal, newInt(1))
		case ddpintlistptr:
			equal := c.cbb.NewCall(c.functions["_ddp_ddpintlist_equal"].irFunc, lhs, rhs)
			c.latestReturn = c.cbb.NewXor(equal, newInt(1))
		case ddpfloatlistptr:
			equal := c.cbb.NewCall(c.functions["_ddp_ddpfloatlist_equal"].irFunc, lhs, rhs)
			c.latestReturn = c.cbb.NewXor(equal, newInt(1))
		case ddpboollistptr:
			equal := c.cbb.NewCall(c.functions["_ddp_ddpboollist_equal"].irFunc, lhs, rhs)
			c.latestReturn = c.cbb.NewXor(equal, newInt(1))
		case ddpcharlistptr:
			equal := c.cbb.NewCall(c.functions["_ddp_ddpcharlist_equal"].irFunc, lhs, rhs)
			c.latestReturn = c.cbb.NewXor(equal, newInt(1))
		case ddpstringlistptr:
			equal := c.cbb.NewCall(c.functions["_ddp_ddpstringlist_equal"].irFunc, lhs, rhs)
			c.latestReturn = c.cbb.NewXor(equal, newInt(1))
		default:
			err("invalid Parameter Types for UNGLEICH (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.KLEINER:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewICmp(enum.IPredSLT, lhs, rhs)
			case ddpfloat:
				fp := c.cbb.NewSIToFP(lhs, ddpfloat)
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOLT, fp, rhs)
			default:
				err("invalid Parameter Types for KLEINER (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				fp := c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOLT, lhs, fp)
			case ddpfloat:
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOLT, lhs, rhs)
			default:
				err("invalid Parameter Types for KLEINER (%s, %s)", lhs.Type(), rhs.Type())
			}
		}
	case token.KLEINERODER:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewICmp(enum.IPredSLE, lhs, rhs)
			case ddpfloat:
				fp := c.cbb.NewSIToFP(lhs, ddpfloat)
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOLE, fp, rhs)
			default:
				err("invalid Parameter Types for KLEINERODER (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				fp := c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOLE, lhs, fp)
			case ddpfloat:
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOLE, lhs, rhs)
			default:
				err("invalid Parameter Types for KLEINERODER (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for KLEINERODER (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.GRÖßER:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewICmp(enum.IPredSGT, lhs, rhs)
			case ddpfloat:
				fp := c.cbb.NewSIToFP(lhs, ddpfloat)
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOGT, fp, rhs)
			default:
				err("invalid Parameter Types for GRÖßER (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				fp := c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOGT, lhs, fp)
			case ddpfloat:
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOGT, lhs, rhs)
			default:
				err("invalid Parameter Types for GRÖßER (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for GRÖßER (%s, %s)", lhs.Type(), rhs.Type())
		}
	case token.GRÖßERODER:
		switch lhs.Type() {
		case ddpint:
			switch rhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewICmp(enum.IPredSGE, lhs, rhs)
			case ddpfloat:
				fp := c.cbb.NewSIToFP(lhs, ddpfloat)
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOGE, fp, rhs)
			default:
				err("invalid Parameter Types for GRÖßERODER (%s, %s)", lhs.Type(), rhs.Type())
			}
		case ddpfloat:
			switch rhs.Type() {
			case ddpint:
				fp := c.cbb.NewSIToFP(rhs, ddpfloat)
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOGE, lhs, fp)
			case ddpfloat:
				c.latestReturn = c.cbb.NewFCmp(enum.FPredOGE, lhs, rhs)
			default:
				err("invalid Parameter Types for GRÖßERODER (%s, %s)", lhs.Type(), rhs.Type())
			}
		default:
			err("invalid Parameter Types for GRÖßERODER (%s, %s)", lhs.Type(), rhs.Type())
		}
	}
	if isDynamic(lhs.Type()) {
		c.freeDynamic(lhs)
	}
	if isDynamic(rhs.Type()) {
		c.freeDynamic(rhs)
	}
}
func (c *Compiler) VisitTernaryExpr(e *ast.TernaryExpr) {
	lhs := c.evaluate(e.Lhs)
	mid := c.evaluate(e.Mid)
	rhs := c.evaluate(e.Rhs)

	switch e.Operator.Type {
	case token.VONBIS:
		switch lhs.Type() {
		case ddpstrptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_slice"].irFunc, lhs, mid, rhs)
		case ddpintlistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpintlist_slice"].irFunc, lhs, mid, rhs)
		case ddpfloatlistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpfloatlist_slice"].irFunc, lhs, mid, rhs)
		case ddpboollistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpboollist_slice"].irFunc, lhs, mid, rhs)
		case ddpcharlistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpcharlist_slice"].irFunc, lhs, mid, rhs)
		case ddpstringlistptr:
			c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpstringlist_slice"].irFunc, lhs, mid, rhs)
		default:
			err("invalid Parameter Types for VONBIS (%s, %s, %s)", lhs.Type(), mid.Type(), rhs.Type())
		}
	default:
		err("invalid Parameter Types for VONBIS (%s, %s, %s)", lhs.Type(), mid.Type(), rhs.Type())
	}

	if isDynamic(lhs.Type()) {
		c.freeDynamic(lhs)
	}
	if isDynamic(mid.Type()) {
		c.freeDynamic(mid)
	}
	if isDynamic(rhs.Type()) {
		c.freeDynamic(rhs)
	}
}
func (c *Compiler) VisitCastExpr(e *ast.CastExpr) {
	lhs := c.evaluate(e.Lhs)
	if e.Type.IsList {
		list := c.cbb.NewCall(c.functions["_ddp_"+getTypeName(e.Type)+"_from_constants"].irFunc, newInt(1))
		arrptr := c.cbb.NewGetElementPtr(derefListPtr(list.Type()), list, newIntT(i32, 0), newIntT(i32, 0))
		arr := c.cbb.NewLoad(ptr(getElementType(list.Type())), arrptr)
		elementPtr := c.cbb.NewGetElementPtr(getElementType(list.Type()), arr, newInt(0))
		c.cbb.NewStore(lhs, elementPtr)
		c.latestReturn = list
		return // don't free lhs
	} else {
		switch e.Type.PrimitiveType {
		case token.ZAHL:
			switch lhs.Type() {
			case ddpint:
				c.latestReturn = lhs
			case ddpfloat:
				c.latestReturn = c.cbb.NewFPToSI(lhs, ddpint)
			case ddpbool:
				cond := c.cbb.NewICmp(enum.IPredNE, lhs, zero)
				c.latestReturn = c.cbb.NewZExt(cond, ddpint)
			case ddpchar:
				c.latestReturn = c.cbb.NewSExt(lhs, ddpint)
			case ddpstrptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_to_int"].irFunc, lhs)
			default:
				err("invalid Parameter Type for ZAHL: %s", lhs.Type())
			}
		case token.KOMMAZAHL:
			switch lhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewSIToFP(lhs, ddpfloat)
			case ddpfloat:
				c.latestReturn = lhs
			case ddpstrptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_string_to_float"].irFunc, lhs)
			default:
				err("invalid Parameter Type for KOMMAZAHL: %s", lhs.Type())
			}
		case token.BOOLEAN:
			switch lhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewICmp(enum.IPredNE, lhs, zero)
			case ddpbool:
				c.latestReturn = lhs
			default:
				err("invalid Parameter Type for BOOLEAN: %s", lhs.Type())
			}
		case token.BUCHSTABE:
			switch lhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewTrunc(lhs, ddpchar)
			case ddpchar:
				c.latestReturn = lhs
			default:
				err("invalid Parameter Type for BUCHSTABE: %s", lhs.Type())
			}
		case token.TEXT:
			switch lhs.Type() {
			case ddpint:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_int_to_string"].irFunc, lhs)
			case ddpfloat:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_float_to_string"].irFunc, lhs)
			case ddpbool:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_bool_to_string"].irFunc, lhs)
			case ddpchar:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_char_to_string"].irFunc, lhs)
			case ddpstrptr:
				c.latestReturn = c.deepCopyDynamic(lhs)
			case ddpintlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpintlist_to_string"].irFunc, lhs)
			case ddpfloatlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpfloatlist_to_string"].irFunc, lhs)
			case ddpboollistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpboollist_to_string"].irFunc, lhs)
			case ddpcharlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpcharlist_to_string"].irFunc, lhs)
			case ddpstringlistptr:
				c.latestReturn = c.cbb.NewCall(c.functions["_ddp_ddpstringlist_to_string"].irFunc, lhs)
			default:
				err("invalid Parameter Type for TEXT: %s", lhs.Type())
			}
		default:
			err("Invalide Typumwandlung zu %s", e.Type)
		}
	}
	if isDynamic(lhs.Type()) {
		c.freeDynamic(lhs)
	}
}
func (c *Compiler) VisitGrouping(e *ast.Grouping) {
	e.Expr.Accept(c) // visit like a normal expression, grouping is just precedence stuff which has already been parsed
}
func (c *Compiler) VisitFuncCall(e *ast.FuncCall) {
	fun := c.functions[e.Name] // retreive the function (the resolver took care that it is present)
	args := make([]value.Value, 0, len(fun.funcDecl.ParamNames))

	for i, param := range fun.funcDecl.ParamNames {
		var val value.Value

		// differentiate between references and normal parameters
		if fun.funcDecl.ParamTypes[i].IsReference {
			switch assign := e.Args[param.Literal].(type) {
			case *ast.Ident:
				val = c.scp.lookupVar(assign.Literal.Literal).val // get the variable
			case *ast.Indexing:
				lhs := c.evaluateAssignable(assign.Lhs) // get the (possibly nested) assignable
				index := c.evaluate(assign.Index)
				switch lhs.Type() {
				case ddpintlistptr, ddpfloatlistptr, ddpboollistptr, ddpcharlistptr, ddpstringlistptr:
					// index into the array
					elementPtr := c.getElementPointer(lhs, index, e)
					val = elementPtr
				default:
					err("invalid Parameter Types for STELLE (%s, %s)", lhs.Type(), index.Type())
				}
			}
		} else {
			val = c.evaluate(e.Args[param.Literal]) // compile each argument for the function
		}

		args = append(args, val) // add the value to the arguments
	}

	c.commentNode(c.cbb, e, "")
	c.latestReturn = c.cbb.NewCall(fun.irFunc, args...) // compile the actual function call

	if ast.IsExternFunc(fun.funcDecl) {
		for i := range fun.funcDecl.ParamNames {
			if !fun.funcDecl.ParamTypes[i].IsReference && isDynamic(toIRType(fun.funcDecl.ParamTypes[i].Type)) {
				c.freeDynamic(args[i])
			}
		}
	}
}

// should have been filtered by the resolver/typechecker, so err
func (c *Compiler) VisitBadStmt(s *ast.BadStmt) {
	err("Es wurde eine invalide Aussage gefunden")
}
func (c *Compiler) VisitDeclStmt(s *ast.DeclStmt) {
	s.Decl.Accept(c)
}
func (c *Compiler) VisitExprStmt(s *ast.ExprStmt) {
	expr := c.evaluate(s.Expr)
	// free garbage collected returns
	if isDynamic(expr.Type()) {
		c.freeDynamic(expr)
	}
}

// helper to resolve nested indexings for VisitAssignStmt
// currently only returns ddpstrptrs as there are no nested lists (yet)
func (c *Compiler) evaluateAssignable(ass ast.Assigneable) value.Value {
	switch assign := ass.(type) {
	case *ast.Ident:
		Var := c.scp.lookupVar(assign.Literal.Literal)
		return c.cbb.NewLoad(Var.typ, Var.val)
	case *ast.Indexing:
		lhs := c.evaluateAssignable(assign.Lhs) // get the (possibly nested) assignable
		index := c.evaluate(assign.Index)
		switch lhs.Type() {
		case ddpstrptr:
			return lhs
		case ddpstringlistptr:
			// index into the array
			elementPtr := c.getElementPointer(lhs, index, ass)
			return c.cbb.NewLoad(elementPtr.ElemType, elementPtr)
		}
	}
	err("Invalid types in evaluateAssignable %s", ass)
	return nil
}

func (c *Compiler) VisitAssignStmt(s *ast.AssignStmt) {
	val := c.evaluate(s.Rhs) // compile the expression
	switch assign := s.Var.(type) {
	case *ast.Ident:
		Var := c.scp.lookupVar(assign.Literal.Literal) // get the variable
		// free the value which was previously contained in the variable
		if isDynamic(Var.typ) {
			c.freeDynamic(c.cbb.NewLoad(Var.typ, Var.val))
		}
		c.commentNode(c.cbb, s, assign.Literal.Literal)
		c.cbb.NewStore(val, Var.val) // store the new value
	case *ast.Indexing:
		lhs := c.evaluateAssignable(assign.Lhs) // get the (possibly nested) assignable
		index := c.evaluate(assign.Index)
		switch lhs.Type() {
		case ddpstrptr:
			c.commentNode(c.cbb, s, "")
			c.cbb.NewCall(c.functions["_ddp_replace_char_in_string"].irFunc, lhs, val, index)
		case ddpintlistptr, ddpfloatlistptr, ddpboollistptr, ddpcharlistptr, ddpstringlistptr:
			// index into the array
			elementPtr := c.getElementPointer(lhs, index, s)
			if lhs.Type() == ddpstringlistptr {
				// free the old string
				c.freeDynamic(c.cbb.NewLoad(getElementType(lhs.Type()), elementPtr))
			}
			c.cbb.NewStore(val, elementPtr)
		default:
			err("invalid Parameter Types for STELLE (%s, %s)", lhs.Type(), index.Type())
		}
	}
}
func (c *Compiler) VisitBlockStmt(s *ast.BlockStmt) {
	c.scp = newScope(c.scp) // a block gets its own scope
	for _, stmt := range s.Statements {
		c.visitNode(stmt)
	}

	c.scp = c.exitScope(c.scp) // free local variables and return to the previous scope
}

// for info on how the generated ir works you might want to see https://llir.github.io/document/user-guide/control/#If
func (c *Compiler) VisitIfStmt(s *ast.IfStmt) {
	cond := c.evaluate(s.Condition)
	thenBlock, elseBlock, leaveBlock := c.cf.NewBlock(""), c.cf.NewBlock(""), c.cf.NewBlock("")
	c.commentNode(c.cbb, s, "")
	if s.Else != nil {
		c.cbb.NewCondBr(cond, thenBlock, elseBlock)
	} else {
		c.cbb.NewCondBr(cond, thenBlock, leaveBlock)
	}

	c.cbb, c.scp = thenBlock, newScope(c.scp)
	c.visitNode(s.Then)
	if c.cbb.Term == nil {
		c.commentNode(c.cbb, s, "")
		c.cbb.NewBr(leaveBlock)
	}
	c.scp = c.exitScope(c.scp)

	if s.Else != nil {
		c.cbb, c.scp = elseBlock, newScope(c.scp)
		c.visitNode(s.Else)
		if c.cbb.Term == nil {
			c.commentNode(c.cbb, s, "")
			c.cbb.NewBr(leaveBlock)
		}
		c.scp = c.exitScope(c.scp)
	} else {
		elseBlock.NewUnreachable()
	}

	c.cbb = leaveBlock
}

// for info on how the generated ir works you might want to see https://llir.github.io/document/user-guide/control/#Loop
func (c *Compiler) VisitWhileStmt(s *ast.WhileStmt) {
	switch op := s.While.Type; op {
	case token.SOLANGE, token.MACHE:
		condBlock := c.cf.NewBlock("")
		body, bodyScope := c.cf.NewBlock(""), newScope(c.scp)

		c.commentNode(c.cbb, s, "")
		if op == token.SOLANGE {
			c.cbb.NewBr(condBlock)
		} else {
			c.cbb.NewBr(body)
		}

		c.cbb, c.scp = body, bodyScope
		c.visitNode(s.Body)
		if c.cbb.Term == nil {
			c.cbb.NewBr(condBlock)
		}

		c.cbb, c.scp = condBlock, c.exitScope(c.scp) // the condition is not in scope
		cond := c.evaluate(s.Condition)
		leaveBlock := c.cf.NewBlock("")
		c.commentNode(c.cbb, s, "")
		c.cbb.NewCondBr(cond, body, leaveBlock)

		c.cbb = leaveBlock
	case token.WIEDERHOLE:
		counter := c.cf.Blocks[0].NewAlloca(ddpint)
		c.cbb.NewStore(c.evaluate(s.Condition), counter)
		condBlock := c.cf.NewBlock("")
		body, bodyScope := c.cf.NewBlock(""), newScope(c.scp)

		c.commentNode(c.cbb, s, "")
		c.cbb.NewBr(condBlock)

		c.cbb, c.scp = body, bodyScope
		c.cbb.NewStore(c.cbb.NewSub(c.cbb.NewLoad(ddpint, counter), newInt(1)), counter)
		c.visitNode(s.Body)
		if c.cbb.Term == nil {
			c.commentNode(c.cbb, s, "")
			c.cbb.NewBr(condBlock)
		}

		leaveBlock := c.cf.NewBlock("")
		c.cbb, c.scp = condBlock, c.exitScope(c.scp) // the condition is not in scope
		c.commentNode(c.cbb, s, "")
		c.cbb.NewCondBr( // while counter != 0, execute body
			c.cbb.NewICmp(enum.IPredNE, c.cbb.NewLoad(ddpint, counter), zero),
			body,
			leaveBlock,
		)

		c.cbb = leaveBlock
	}
}

// for info on how the generated ir works you might want to see https://llir.github.io/document/user-guide/control/#Loop
func (c *Compiler) VisitForStmt(s *ast.ForStmt) {
	c.scp = newScope(c.scp)     // scope for the for body
	c.visitNode(s.Initializer)  // compile the counter variable declaration
	initValue := c.latestReturn // safe the initial value of the counter to check for less or greater then

	condBlock := c.cf.NewBlock("")
	c.comment("condBlock", condBlock)
	incrementBlock := c.cf.NewBlock("")
	c.comment("incrementBlock", incrementBlock)
	forBody := c.cf.NewBlock("")
	c.comment("forBody", forBody)

	c.commentNode(c.cbb, s, "")
	c.cbb.NewBr(condBlock) // we begin by evaluating the condition (not compiled yet, but the ir starts here)
	// compile the for-body
	c.cbb = forBody
	c.visitNode(s.Body)
	if c.cbb.Term == nil { // if there is no return at the end we jump to the incrementBlock
		c.commentNode(c.cbb, s, "")
		c.cbb.NewBr(incrementBlock)
	}

	// compile the incrementBlock
	Var := c.scp.lookupVar(s.Initializer.Name.Literal)
	indexVar := incrementBlock.NewLoad(Var.typ, Var.val)
	var incrementer value.Value // Schrittgröße
	// if no stepsize was present it is 1
	if s.StepSize == nil {
		incrementer = newInt(1)
	} else { // stepsize was present, so compile it
		c.cbb = incrementBlock
		incrementer = c.evaluate(s.StepSize)
	}

	c.cbb = incrementBlock
	// add the incrementer to the counter variable
	add := c.cbb.NewAdd(indexVar, incrementer)
	c.cbb.NewStore(add, c.scp.lookupVar(s.Initializer.Name.Literal).val)
	c.commentNode(c.cbb, s, "")
	c.cbb.NewBr(condBlock) // check the condition (loop)

	// finally compile the condition block(s)
	initGreaterTo := c.cf.NewBlock("")
	c.comment("initGreaterTo", initGreaterTo)
	initLessthenTo := c.cf.NewBlock("")
	c.comment("initLessthenTo", initLessthenTo)
	leaveBlock := c.cf.NewBlock("") // after the condition is false we jump to the leaveBlock
	c.comment("forLeaveBlock", leaveBlock)

	c.cbb = condBlock
	// we check the counter differently depending on wether or not we are looping up or down (positive vs negative stepsize)
	cond := c.cbb.NewICmp(enum.IPredSLE, initValue, c.evaluate(s.To))
	c.commentNode(c.cbb, s, "")
	c.cbb.NewCondBr(cond, initLessthenTo, initGreaterTo)

	c.cbb = initLessthenTo
	// we are counting up, so compare less-or-equal
	cond = c.cbb.NewICmp(enum.IPredSLE, c.cbb.NewLoad(Var.typ, Var.val), c.evaluate(s.To))
	c.commentNode(c.cbb, s, "")
	c.cbb.NewCondBr(cond, forBody, leaveBlock)

	c.cbb = initGreaterTo
	// we are counting down, so compare greater-or-equal
	cond = c.cbb.NewICmp(enum.IPredSGE, c.cbb.NewLoad(Var.typ, Var.val), c.evaluate(s.To))
	c.commentNode(c.cbb, s, "")
	c.cbb.NewCondBr(cond, forBody, leaveBlock)

	c.cbb, c.scp = leaveBlock, c.exitScope(c.scp) // leave the scopee
}
func (c *Compiler) VisitForRangeStmt(s *ast.ForRangeStmt) {
	c.scp = newScope(c.scp)
	in := c.scp.addDynamic(c.evaluate(s.In))

	var len value.Value
	if in.Type() == ddpstrptr {
		len = c.cbb.NewCall(c.functions["_ddp_string_length"].irFunc, in)
	} else {
		lenptr := c.cbb.NewGetElementPtr(derefListPtr(in.Type()), in, newIntT(i32, 0), newIntT(i32, 1))
		len = c.cbb.NewLoad(ddpint, lenptr)
	}
	loopStart, condBlock, bodyBlock, incrementBlock, leaveBlock := c.cf.NewBlock(""), c.cf.NewBlock(""), c.cf.NewBlock(""), c.cf.NewBlock(""), c.cf.NewBlock("")
	c.cbb.NewCondBr(c.cbb.NewICmp(enum.IPredEQ, len, zero), leaveBlock, loopStart)

	c.cbb = loopStart
	index := c.cf.Blocks[0].NewAlloca(ddpint)
	c.cbb.NewStore(newInt(1), index)
	irType := toIRType(s.Initializer.Type)
	c.scp.addVar(s.Initializer.Name.Literal, c.cf.Blocks[0].NewAlloca(irType), irType, false)
	c.cbb.NewBr(condBlock)

	c.cbb = condBlock
	c.cbb.NewCondBr(c.cbb.NewICmp(enum.IPredSLE, c.cbb.NewLoad(ddpint, index), len), bodyBlock, leaveBlock)

	c.cbb = bodyBlock
	var loopVar value.Value
	if in.Type() == ddpstrptr {
		loopVar = c.cbb.NewCall(c.functions["_ddp_string_index"].irFunc, in, c.cbb.NewLoad(ddpint, index))
	} else {
		arrptr := c.cbb.NewGetElementPtr(derefListPtr(in.Type()), in, newIntT(i32, 0), newIntT(i32, 0))
		arr := c.cbb.NewLoad(ptr(getElementType(in.Type())), arrptr)
		ddpindex := c.cbb.NewSub(c.cbb.NewLoad(ddpint, index), newInt(1))
		elementPtr := c.cbb.NewGetElementPtr(getElementType(in.Type()), arr, ddpindex)
		loopVar = c.cbb.NewLoad(getElementType(in.Type()), elementPtr)
		// copy strings
		if in.Type() == ddpstringlistptr {
			loopVar = c.deepCopyDynamic(loopVar)
		}
	}
	c.cbb.NewStore(loopVar, c.scp.lookupVar(s.Initializer.Name.Literal).val)
	c.visitNode(s.Body)
	if c.cbb.Term == nil {
		c.cbb.NewBr(incrementBlock)
	}

	c.cbb = incrementBlock
	c.cbb.NewStore(c.cbb.NewAdd(c.cbb.NewLoad(ddpint, index), newInt(1)), index)
	c.cbb.NewBr(condBlock)

	c.cbb, c.scp = leaveBlock, c.exitScope(c.scp)
	c.freeDynamic(in)
}
func (c *Compiler) VisitReturnStmt(s *ast.ReturnStmt) {
	if s.Value == nil {
		c.exitNestedScopes()
		c.commentNode(c.cbb, s, "")
		c.cbb.NewRet(nil)
		return
	}
	val := c.evaluate(s.Value)
	c.exitNestedScopes()
	c.commentNode(c.cbb, s, "")
	c.cbb.NewRet(val)
}

func (c *Compiler) exitNestedScopes() {
	for scp := c.scp; scp != c.cfscp; scp = c.exitScope(scp) {
		for i := range scp.dynamics {
			c.freeDynamic(scp.dynamics[i])
		}
	}
	c.exitScope(c.cfscp)
}
