package ast

import (
	"fmt"

	"github.com/DDP-Projekt/Kompilierer/pkg/token"
)

// simple visitor to print an AST
type printer struct {
	currentIdent int
	returned     string
}

// print the AST to stdout
func (ast *Ast) Print() {
	printer := &printer{}
	WalkAst(ast, printer)
	fmt.Println(printer.returned)
}

func (ast *Ast) String() string {
	printer := &printer{}
	WalkAst(ast, printer)
	return printer.returned
}

func (pr *printer) printIdent() {
	for i := 0; i < pr.currentIdent; i++ {
		pr.print("   ")
	}
}

func (pr *printer) print(str string) {
	pr.returned += str
}

func (pr *printer) parenthesizeNode(name string, nodes ...Node) string {
	pr.print("(" + name)
	pr.currentIdent++

	for _, node := range nodes {
		pr.print("\n")
		pr.printIdent()
		node.Accept(pr)
	}

	pr.currentIdent--
	if len(nodes) != 0 {
		pr.printIdent()
	}

	pr.print(")\n")
	return pr.returned
}

func (pr *printer) VisitBadDecl(decl *BadDecl) {
	pr.parenthesizeNode(fmt.Sprintf("BadDecl[%s]", decl.Tok))
}
func (pr *printer) VisitVarDecl(decl *VarDecl) {
	pr.parenthesizeNode(fmt.Sprintf("VarDecl[%s]", decl.Name.Literal), decl.InitVal)
}
func (pr *printer) VisitFuncDecl(decl *FuncDecl) {
	if IsExternFunc(decl) {
		pr.parenthesizeNode(fmt.Sprintf("FuncDecl[%s: %v, %v, %s] Extern", decl.Name.Literal, tokenSlice(decl.ParamNames).literals(), decl.ParamTypes, decl.Type))
	} else {
		pr.parenthesizeNode(fmt.Sprintf("FuncDecl[%s: %v, %v, %s]", decl.Name.Literal, tokenSlice(decl.ParamNames).literals(), decl.ParamTypes, decl.Type), decl.Body)
	}
}

func (pr *printer) VisitBadExpr(expr *BadExpr) {
	pr.parenthesizeNode(fmt.Sprintf("BadExpr[%s]", expr.Tok))
}
func (pr *printer) VisitIdent(expr *Ident) {
	pr.parenthesizeNode(fmt.Sprintf("Ident[%s]", expr.Literal.Literal))
}
func (pr *printer) VisitIndexing(expr *Indexing) {
	pr.parenthesizeNode("Indexing", expr.Lhs, expr.Index)
}
func (pr *printer) VisitIntLit(expr *IntLit) {
	pr.parenthesizeNode(fmt.Sprintf("IntLit(%d)", expr.Value))
}
func (pr *printer) VisitFloatLit(expr *FloatLit) {
	pr.parenthesizeNode(fmt.Sprintf("FloatLit(%f)", expr.Value))
}
func (pr *printer) VisitBoolLit(expr *BoolLit) {
	pr.parenthesizeNode(fmt.Sprintf("BoolLit(%v)", expr.Value))
}
func (pr *printer) VisitCharLit(expr *CharLit) {
	pr.parenthesizeNode(fmt.Sprintf("CharLit(%c)", expr.Value))
}
func (pr *printer) VisitStringLit(expr *StringLit) {
	pr.parenthesizeNode(fmt.Sprintf("StringLit[%s]", expr.Token().Literal))
}
func (pr *printer) VisitListLit(expr *ListLit) {
	if expr.Values == nil {
		pr.parenthesizeNode(fmt.Sprintf("ListLit[%s]", expr.Type))
	} else {
		nodes := make([]Node, 0, len(expr.Values))
		for _, v := range expr.Values {
			nodes = append(nodes, v)
		}
		pr.parenthesizeNode("ListLit", nodes...)
	}
}
func (pr *printer) VisitUnaryExpr(expr *UnaryExpr) {
	pr.parenthesizeNode(fmt.Sprintf("UnaryExpr[%s]", expr.Operator), expr.Rhs)
}
func (pr *printer) VisitBinaryExpr(expr *BinaryExpr) {
	pr.parenthesizeNode(fmt.Sprintf("BinaryExpr[%s]", expr.Operator), expr.Lhs, expr.Rhs)
}
func (pr *printer) VisitTernaryExpr(expr *TernaryExpr) {
	pr.parenthesizeNode(fmt.Sprintf("TernaryExpr[%s]", expr.Operator), expr.Lhs, expr.Mid, expr.Rhs)
}
func (pr *printer) VisitCastExpr(expr *CastExpr) {
	pr.parenthesizeNode(fmt.Sprintf("CastExpr[%s]", expr.Type), expr.Lhs)
}
func (pr *printer) VisitGrouping(expr *Grouping) {
	pr.parenthesizeNode("Grouping", expr.Expr)
}
func (pr *printer) VisitFuncCall(expr *FuncCall) {
	args := make([]Node, 0)
	for _, v := range expr.Args {
		args = append(args, v)
	}
	pr.parenthesizeNode(fmt.Sprintf("FuncCall(%s)", expr.Name), args...)
}

func (pr *printer) VisitBadStmt(stmt *BadStmt) {
	pr.parenthesizeNode(fmt.Sprintf("BadStmt[%s]", stmt.Tok))
}
func (pr *printer) VisitDeclStmt(stmt *DeclStmt) {
	pr.parenthesizeNode("DeclStmt", stmt.Decl)
}
func (pr *printer) VisitExprStmt(stmt *ExprStmt) {
	pr.parenthesizeNode("ExprStmt", stmt.Expr)
}
func (pr *printer) VisitAssignStmt(stmt *AssignStmt) {
	pr.parenthesizeNode("AssignStmt", stmt.Var, stmt.Rhs)
}
func (pr *printer) VisitBlockStmt(stmt *BlockStmt) {
	args := make([]Node, len(stmt.Statements))
	for i, v := range stmt.Statements {
		args[i] = v
	}
	pr.parenthesizeNode("BlockStmt", args...)
}
func (pr *printer) VisitIfStmt(stmt *IfStmt) {
	if stmt.Else != nil {
		pr.parenthesizeNode("IfStmt", stmt.Condition, stmt.Then, stmt.Else)
	} else {
		pr.parenthesizeNode("IfStmt", stmt.Condition, stmt.Then)
	}
}
func (pr *printer) VisitWhileStmt(stmt *WhileStmt) {
	pr.parenthesizeNode("WhileStmt", stmt.Condition, stmt.Body)
}
func (pr *printer) VisitForStmt(stmt *ForStmt) {
	pr.parenthesizeNode("ForStmt", stmt.Initializer, stmt.To, stmt.StepSize, stmt.Body)
}
func (pr *printer) VisitForRangeStmt(stmt *ForRangeStmt) {
	pr.parenthesizeNode("ForRangeStmt", stmt.Initializer, stmt.In, stmt.Body)
}
func (pr *printer) VisitReturnStmt(stmt *ReturnStmt) {
	if stmt.Value == nil {
		pr.parenthesizeNode("ReturnStmt[void]")
	} else {
		pr.parenthesizeNode("ReturnStmt", stmt.Value)
	}
}

type tokenSlice []token.Token

func (tokens tokenSlice) literals() []string {
	result := make([]string, 0, len(tokens))
	for _, v := range tokens {
		result = append(result, v.Literal)
	}
	return result
}
