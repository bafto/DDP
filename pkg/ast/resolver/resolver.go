package resolver

import (
	"fmt"

	"github.com/DDP-Projekt/Kompilierer/pkg/ast"
	"github.com/DDP-Projekt/Kompilierer/pkg/ddperror"
	"github.com/DDP-Projekt/Kompilierer/pkg/token"
)

// holds state to resolve the symbols of an AST and its nodes
// and checking if they are valid
// fills the ASTs SymbolTable while doing so
type Resolver struct {
	ErrorHandler  ddperror.Handler // function to which errors are passed
	CurrentTable  *ast.SymbolTable // needed state, public for the parser
	Errored       bool             // wether the resolver errored
	ResolveBlocks bool             // wether to resolve blockStatements
}

// create a new resolver to resolve the passed AST
func New(ast *ast.Ast, errorHandler ddperror.Handler) *Resolver {
	if errorHandler == nil {
		errorHandler = ddperror.EmptyHandler
	}
	return &Resolver{
		ErrorHandler:  errorHandler,
		CurrentTable:  ast.Symbols,
		Errored:       false,
		ResolveBlocks: true,
	}
}

// fills out the asts SymbolTables and reports any errors on the way
func ResolveAst(Ast *ast.Ast, errorHandler ddperror.Handler) {
	Ast.Symbols = ast.NewSymbolTable(nil) // reset the ASTs symbols

	resolver := New(Ast, errorHandler)

	// visit all nodes of the AST
	for i, l := 0, len(Ast.Statements); i < l; i++ {
		Ast.Statements[i].Accept(resolver)
	}

	// if the resolver errored, the AST is not valid DDP code
	if resolver.Errored {
		Ast.Faulty = true
	}
}

// resolve a single node
func (r *Resolver) ResolveNode(node ast.Node, resolveBlocks bool) *Resolver {
	r.ResolveBlocks = resolveBlocks
	return node.Accept(r).(*Resolver)
}

// helper to visit a node
func (r *Resolver) visit(node ast.Node) {
	node.Accept(r)
}

// helper for errors
func (r *Resolver) err(tok token.Token, msg string, args ...any) {
	r.Errored = true
	r.ErrorHandler(&ResolverError{file: tok.File, rang: tok.Range, msg: fmt.Sprintf(msg, args...)})
}

// if a BadDecl exists the AST is faulty
func (r *Resolver) VisitBadDecl(decl *ast.BadDecl) ast.FullVisitor {
	r.Errored = true
	return r
}
func (r *Resolver) VisitVarDecl(decl *ast.VarDecl) ast.FullVisitor {
	decl.InitVal.Accept(r) // resolve the initial value
	// insert the variable into the current scope (SymbolTable)
	if existed := r.CurrentTable.InsertVar(decl.Name.Literal, decl); existed {
		r.err(decl.Name, "Die Variable '%s' existiert bereits", decl.Name.Literal) // variables may only be declared once in the same scope
	}

	return r
}
func (r *Resolver) VisitFuncDecl(decl *ast.FuncDecl) ast.FullVisitor {
	if existed := r.CurrentTable.InsertFunc(decl.Name.Literal, decl); existed {
		r.err(decl.Name, "Die Funktion '%s' existiert bereits", decl.Name.Literal) // functions may only be declared once
	}
	if !ast.IsExternFunc(decl) {
		decl.Body.Symbols = ast.NewSymbolTable(r.CurrentTable) // create a new scope for the function body
		// add the function parameters to the scope of the function body
		for i, l := 0, len(decl.ParamNames); i < l; i++ {
			decl.Body.Symbols.InsertVar(decl.ParamNames[i].Literal, &ast.VarDecl{Name: decl.ParamNames[i], Type: decl.ParamTypes[i].Type, Range: token.NewRange(decl.ParamNames[i], decl.ParamNames[i])})
		}

		return decl.Body.Accept(r) // resolve the function body
	}
	return r
}

// if a BadExpr exists the AST is faulty
func (r *Resolver) VisitBadExpr(expr *ast.BadExpr) ast.FullVisitor {
	r.Errored = true
	return r
}
func (r *Resolver) VisitIdent(expr *ast.Ident) ast.FullVisitor {
	// check if the variable exists
	if _, exists := r.CurrentTable.LookupVar(expr.Literal.Literal); !exists {
		r.err(expr.Token(), "Der Name '%s' wurde noch nicht als Variable oder Funktions-Alias deklariert", expr.Literal.Literal)
	}
	return r
}
func (r *Resolver) VisitIndexing(expr *ast.Indexing) ast.FullVisitor {
	r.visit(expr.Lhs)
	return expr.Index.Accept(r)
}

// nothing to do for literals
func (r *Resolver) VisitIntLit(expr *ast.IntLit) ast.FullVisitor {
	return r
}
func (r *Resolver) VisitFloatLit(expr *ast.FloatLit) ast.FullVisitor {
	return r
}
func (r *Resolver) VisitBoolLit(expr *ast.BoolLit) ast.FullVisitor {
	return r
}
func (r *Resolver) VisitCharLit(expr *ast.CharLit) ast.FullVisitor {
	return r
}
func (r *Resolver) VisitStringLit(expr *ast.StringLit) ast.FullVisitor {
	return r
}
func (r *Resolver) VisitListLit(expr *ast.ListLit) ast.FullVisitor {
	if expr.Values != nil {
		for _, v := range expr.Values {
			r.visit(v)
		}
	} else if expr.Count != nil && expr.Value != nil {
		r.visit(expr.Count)
		r.visit(expr.Value)
	}
	return r
}
func (r *Resolver) VisitUnaryExpr(expr *ast.UnaryExpr) ast.FullVisitor {
	return expr.Rhs.Accept(r) // visit the actual expression
}
func (r *Resolver) VisitBinaryExpr(expr *ast.BinaryExpr) ast.FullVisitor {
	return expr.Rhs.Accept(expr.Lhs.Accept(r)) // visit the actual expressions
}
func (r *Resolver) VisitTernaryExpr(expr *ast.TernaryExpr) ast.FullVisitor {
	return expr.Rhs.Accept(expr.Mid.Accept(expr.Lhs.Accept(r))) // visit the actual expressions
}
func (r *Resolver) VisitCastExpr(expr *ast.CastExpr) ast.FullVisitor {
	return expr.Lhs.Accept(r) // visit the actual expressions
}
func (r *Resolver) VisitGrouping(expr *ast.Grouping) ast.FullVisitor {
	return expr.Expr.Accept(r)
}
func (r *Resolver) VisitFuncCall(expr *ast.FuncCall) ast.FullVisitor {
	// visit the passed arguments
	for _, v := range expr.Args {
		r.visit(v)
	}
	return r
}

// if a BadStmt exists the AST is faulty
func (r *Resolver) VisitBadStmt(stmt *ast.BadStmt) ast.FullVisitor {
	r.Errored = true
	return r
}
func (r *Resolver) VisitDeclStmt(stmt *ast.DeclStmt) ast.FullVisitor {
	return stmt.Decl.Accept(r)
}
func (r *Resolver) VisitExprStmt(stmt *ast.ExprStmt) ast.FullVisitor {
	return stmt.Expr.Accept(r)
}
func (r *Resolver) VisitAssignStmt(stmt *ast.AssignStmt) ast.FullVisitor {
	switch assign := stmt.Var.(type) {
	case *ast.Ident:
		// check if the variable exists
		if _, exists := r.CurrentTable.LookupVar(assign.Literal.Literal); !exists {
			r.err(stmt.Token(), "Der Name '%s' wurde in noch nicht als Variable deklariert", assign.Literal.Literal)
		}
	case *ast.Indexing:
		r.visit(assign.Lhs)
		r.visit(assign.Index)
	}

	return stmt.Rhs.Accept(r)
}
func (r *Resolver) VisitBlockStmt(stmt *ast.BlockStmt) ast.FullVisitor {
	if r.ResolveBlocks {
		// a block needs a new scope
		if stmt.Symbols == nil {
			stmt.Symbols = ast.NewSymbolTable(r.CurrentTable)
		}
		r.CurrentTable = stmt.Symbols // set the current scope to the block

		// visit every statement in the block
		for _, stmt := range stmt.Statements {
			r.visit(stmt)
		}

		r.CurrentTable = stmt.Symbols.Enclosing
	}
	return r
}
func (r *Resolver) VisitIfStmt(stmt *ast.IfStmt) ast.FullVisitor {
	r.visit(stmt.Condition)
	r.visit(stmt.Then)
	if stmt.Else != nil {
		r.visit(stmt.Else)
	}

	return r
}
func (r *Resolver) VisitWhileStmt(stmt *ast.WhileStmt) ast.FullVisitor {
	r.visit(stmt.Condition)
	return stmt.Body.Accept(r)
}
func (r *Resolver) VisitForStmt(stmt *ast.ForStmt) ast.FullVisitor {
	var env *ast.SymbolTable // scope of the for loop
	// if it contains a block statement, the counter variable needs to go in there
	if body, ok := stmt.Body.(*ast.BlockStmt); ok {
		body.Symbols = ast.NewSymbolTable(r.CurrentTable)
		env = body.Symbols
	} else { // otherwise, just a new scope
		env = ast.NewSymbolTable(r.CurrentTable)
	}

	r.CurrentTable = env
	r.visit(stmt.Initializer)
	r.visit(stmt.To)
	if stmt.StepSize != nil {
		r.visit(stmt.StepSize)
	}
	r.visit(stmt.Body)
	r.CurrentTable = env.Enclosing

	return r
}
func (r *Resolver) VisitForRangeStmt(stmt *ast.ForRangeStmt) ast.FullVisitor {
	var env *ast.SymbolTable // scope of the for loop
	// if it contains a block statement, the counter variable needs to go in there
	if body, ok := stmt.Body.(*ast.BlockStmt); ok {
		body.Symbols = ast.NewSymbolTable(r.CurrentTable)
		env = body.Symbols
	} else { // otherwise, just a new scope
		env = ast.NewSymbolTable(r.CurrentTable)
	}

	r.CurrentTable = env
	r.visit(stmt.Initializer) // also visits stmt.In
	r.visit(stmt.Body)
	r.CurrentTable = env.Enclosing

	return r
}
func (r *Resolver) VisitReturnStmt(stmt *ast.ReturnStmt) ast.FullVisitor {
	if _, exists := r.CurrentTable.LookupFunc(stmt.Func); !exists {
		r.err(stmt.Token(), "Man kann nur aus Funktionen einen Wert zurückgeben")
	}
	if stmt.Value == nil {
		return r
	}
	return stmt.Value.Accept(r)
}
