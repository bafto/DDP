package typechecker

import (
	"fmt"

	"github.com/DDP-Projekt/Kompilierer/pkg/ast"
	"github.com/DDP-Projekt/Kompilierer/pkg/ddperror"
	"github.com/DDP-Projekt/Kompilierer/pkg/token"
)

// holds state to check if the types of an AST are valid
type Typechecker struct {
	ErrorHandler       ddperror.Handler // function to which errors are passed
	CurrentTable       *ast.SymbolTable // SymbolTable of the current scope (needed for name type-checking)
	Errored            bool             // wether the typechecker found an error
	CheckBlocks        bool             // wether to typecheck blockStatements
	latestReturnedType token.DDPType    // type of the last visited expression
}

func New(symbols *ast.SymbolTable, errorHandler ddperror.Handler) *Typechecker {
	if errorHandler == nil {
		errorHandler = ddperror.EmptyHandler
	}
	return &Typechecker{
		ErrorHandler:       errorHandler,
		CurrentTable:       symbols,
		Errored:            false,
		CheckBlocks:        true,
		latestReturnedType: token.DDPVoidType(),
	}
}

// checks that all ast nodes fulfill type requirements
func TypecheckAst(Ast *ast.Ast, errorHandler ddperror.Handler) {
	typechecker := New(Ast.Symbols, errorHandler)

	for i, l := 0, len(Ast.Statements); i < l; i++ {
		Ast.Statements[i].Accept(typechecker)
	}

	if typechecker.Errored {
		Ast.Faulty = true
	}
}

// typecheck a single node
func (t *Typechecker) TypecheckNode(node ast.Node, checkBlocks bool) {
	t.CheckBlocks = checkBlocks
	node.Accept(t)
}

// helper to visit a node
func (t *Typechecker) visit(node ast.Node) {
	node.Accept(t)
}

// Evaluates the type of an expression
func (t *Typechecker) Evaluate(expr ast.Expression) token.DDPType {
	t.visit(expr)
	return t.latestReturnedType
}

// helper for errors
func (t *Typechecker) err(tok token.Token, msg string, args ...any) {
	t.Errored = true
	t.ErrorHandler(&TypecheckerError{file: tok.File, rang: tok.Range, msg: fmt.Sprintf(msg, args...)})
}

// helper for commmon error message
func (t *Typechecker) errExpected(tok token.Token, got token.DDPType, expected ...token.DDPType) {
	msg := fmt.Sprintf("Der %s Operator erwartet einen Ausdruck vom Typ ", tok)
	if len(expected) == 1 {
		msg = fmt.Sprintf("Der %s Operator erwartet einen Ausdruck vom Typ %s aber hat '%s' bekommen", tok, expected[0], got)
	} else {
		for i, v := range expected {
			if i >= len(expected)-1 {
				break
			}
			msg += fmt.Sprintf("'%s', ", v)
		}
		msg += fmt.Sprintf("oder '%s' aber hat '%s' bekommen", expected[len(expected)-1], got)
	}
	t.err(tok, msg)
}

// helper for commmon error message
func (t *Typechecker) errExpectedBin(tok token.Token, t1, t2 token.DDPType, op token.TokenType) {
	t.err(tok, "Die Typen Kombination aus '%s' und '%s' passt nicht zu dem '%s' Operator", t1, t2, op)
}

// helper for commmon error message
func (t *Typechecker) errExpectedTern(tok token.Token, t1, t2, t3 token.DDPType, op token.TokenType) {
	t.err(tok, "Die Typen Kombination aus '%s', '%s' und '%s' passt nicht zu dem '%s' Operator", t1, t2, t3, op)
}

func (*Typechecker) BaseVisitor() {}

func (t *Typechecker) VisitBadDecl(decl *ast.BadDecl) {
	t.Errored = true
	t.latestReturnedType = token.DDPVoidType()
}
func (t *Typechecker) VisitVarDecl(decl *ast.VarDecl) {
	initialType := t.Evaluate(decl.InitVal)
	if initialType != decl.Type {
		t.err(decl.InitVal.Token(),
			"Ein Wert vom Typ %s kann keiner Variable vom Typ %s zugewiesen werden",
			initialType,
			decl.Type,
		)
	}
}
func (t *Typechecker) VisitFuncDecl(decl *ast.FuncDecl) {
	if !ast.IsExternFunc(decl) {
		decl.Body.Accept(t)
	}
}

func (t *Typechecker) VisitBadExpr(expr *ast.BadExpr) {
	t.Errored = true
	t.latestReturnedType = token.DDPVoidType()
}
func (t *Typechecker) VisitIdent(expr *ast.Ident) {
	decl, ok := t.CurrentTable.LookupVar(expr.Literal.Literal)
	if !ok {
		t.latestReturnedType = token.DDPVoidType()
	} else {
		t.latestReturnedType = decl.Type
	}
}
func (t *Typechecker) VisitIndexing(expr *ast.Indexing) {
	if typ := t.Evaluate(expr.Index); typ != token.DDPIntType() {
		t.err(expr.Index.Token(), "Der STELLE Operator erwartet eine Zahl als zweiten Operanden, nicht %s", typ)
	}

	lhs := t.Evaluate(expr.Lhs)
	if !lhs.IsList && lhs.PrimitiveType != token.TEXT {
		t.err(expr.Lhs.Token(), "Der STELLE Operator erwartet einen Text oder eine Liste als ersten Operanden, nicht %s", lhs)
	}

	if lhs.IsList {
		t.latestReturnedType = token.NewPrimitiveType(lhs.PrimitiveType)
	} else {
		t.latestReturnedType = token.DDPCharType() // later on the list element type
	}
}
func (t *Typechecker) VisitIntLit(expr *ast.IntLit) {
	t.latestReturnedType = token.DDPIntType()
}
func (t *Typechecker) VisitFloatLit(expr *ast.FloatLit) {
	t.latestReturnedType = token.DDPFloatType()
}
func (t *Typechecker) VisitBoolLit(expr *ast.BoolLit) {
	t.latestReturnedType = token.DDPBoolType()
}
func (t *Typechecker) VisitCharLit(expr *ast.CharLit) {
	t.latestReturnedType = token.DDPCharType()
}
func (t *Typechecker) VisitStringLit(expr *ast.StringLit) {
	t.latestReturnedType = token.DDPStringType()
}
func (t *Typechecker) VisitListLit(expr *ast.ListLit) {
	if expr.Values != nil {
		elementType := t.Evaluate(expr.Values[0])
		for _, v := range expr.Values[1:] {
			if ty := t.Evaluate(v); elementType != ty {
				t.err(v.Token(), "Falscher Typ (%s) in Listen Literal vom Typ %s", ty, elementType)
			}
		}
		expr.Type = token.NewListType(elementType.PrimitiveType)
	} else if expr.Count != nil && expr.Value != nil {
		if count := t.Evaluate(expr.Count); count != token.DDPIntType() {
			t.err(expr.Count.Token(), "Die Größe einer Liste muss als Zahl angegeben werden, nicht als %s", count)
		}
		if val := t.Evaluate(expr.Value); val != token.NewPrimitiveType(expr.Type.PrimitiveType) {
			t.err(expr.Value.Token(), "Falscher Typ (%s) in Listen Literal vom Typ %s", val, token.NewPrimitiveType(expr.Type.PrimitiveType))
		}
	}
	t.latestReturnedType = expr.Type
}
func (t *Typechecker) VisitUnaryExpr(expr *ast.UnaryExpr) {
	// Evaluate the rhs expression and check if the operator fits it
	rhs := t.Evaluate(expr.Rhs)
	switch expr.Operator.Type {
	case token.BETRAG, token.NEGATE:
		if !rhs.IsNumeric() {
			t.errExpected(expr.Operator, rhs, token.DDPIntType(), token.DDPFloatType())
		}
	case token.NICHT:
		if !isOfType(rhs, token.DDPBoolType()) {
			t.errExpected(expr.Operator, rhs, token.DDPBoolType())
		}

		t.latestReturnedType = token.DDPBoolType()
	case token.NEGIERE:
		if !isOfType(rhs, token.DDPBoolType(), token.DDPIntType()) {
			t.errExpected(expr.Operator, rhs, token.DDPBoolType(), token.DDPIntType())
		}
	case token.LOGISCHNICHT:
		if !isOfType(rhs, token.DDPIntType()) {
			t.errExpected(expr.Operator, rhs, token.DDPIntType())
		}

		t.latestReturnedType = token.DDPIntType()
	case token.LÄNGE:
		if !rhs.IsList && rhs.PrimitiveType != token.TEXT {
			t.err(expr.Token(), "Der LÄNGE Operator erwartet einen Text oder eine Liste als Operanden, nicht %s", rhs)
		}

		t.latestReturnedType = token.DDPIntType()
	case token.GRÖßE:
		t.latestReturnedType = token.DDPIntType()
	default:
		t.err(expr.Operator, "Unbekannter unärer Operator '%s'", expr.Operator)
	}
}
func (t *Typechecker) VisitBinaryExpr(expr *ast.BinaryExpr) {
	lhs := t.Evaluate(expr.Lhs)
	rhs := t.Evaluate(expr.Rhs)

	// helper to validate if types match
	validate := func(op token.TokenType, valid ...token.DDPType) {
		if !isOfTypeBin(lhs, rhs, valid...) {
			t.errExpectedBin(expr.Token(), lhs, rhs, op)
		}
	}

	switch op := expr.Operator.Type; op {
	case token.VERKETTET:
		if (!lhs.IsList && !rhs.IsList) && (lhs == token.DDPStringType() || rhs == token.DDPStringType()) { // string, char edge case
			validate(expr.Operator.Type, token.DDPStringType(), token.DDPCharType())
			t.latestReturnedType = token.DDPStringType()
		} else { // lists
			if lhs.PrimitiveType != rhs.PrimitiveType {
				t.err(expr.Operator, "Die Typenkombination aus %s und %s passt nicht zum VERKETTET Operator", lhs, rhs)
			}
			t.latestReturnedType = token.NewListType(lhs.PrimitiveType)
		}
	case token.PLUS, token.ADDIERE, token.ERHÖHE,
		token.MINUS, token.SUBTRAHIERE, token.VERRINGERE,
		token.MAL, token.MULTIPLIZIERE, token.VERVIELFACHE:
		validate(op, token.DDPIntType(), token.DDPFloatType())

		if lhs == token.DDPIntType() && rhs == token.DDPIntType() {
			t.latestReturnedType = token.DDPIntType()
		} else {
			t.latestReturnedType = token.DDPFloatType()
		}
	case token.STELLE:
		if !lhs.IsList && lhs != token.DDPStringType() {
			t.err(expr.Lhs.Token(), "Der STELLE Operator erwartet einen Text oder eine Liste als ersten Operanden, nicht %s", lhs)
		}
		if rhs != token.DDPIntType() {
			t.err(expr.Lhs.Token(), "Der STELLE Operator erwartet eine Zahl als zweiten Operanden, nicht %s", rhs)
		}

		if lhs.IsList {
			t.latestReturnedType = token.NewPrimitiveType(lhs.PrimitiveType)
		} else if lhs == token.DDPStringType() {
			t.latestReturnedType = token.DDPCharType() // later on the list element type
		}
	case token.DURCH, token.DIVIDIERE, token.TEILE, token.HOCH, token.LOGARITHMUS:
		validate(op, token.DDPIntType(), token.DDPFloatType())
		t.latestReturnedType = token.DDPFloatType()
	case token.MODULO:
		validate(op, token.DDPIntType())
		t.latestReturnedType = token.DDPIntType()
	case token.UND:
		validate(op, token.DDPBoolType())
		t.latestReturnedType = token.DDPBoolType()
	case token.ODER:
		validate(op, token.DDPBoolType())
		t.latestReturnedType = token.DDPBoolType()
	case token.LINKS:
		validate(op, token.DDPIntType())
		t.latestReturnedType = token.DDPIntType()
	case token.RECHTS:
		validate(op, token.DDPIntType())
		t.latestReturnedType = token.DDPIntType()
	case token.GLEICH:
		if lhs != rhs {
			t.errExpectedBin(expr.Token(), lhs, rhs, op)
		}

		t.latestReturnedType = token.DDPBoolType()
	case token.UNGLEICH:
		if lhs != rhs {
			t.errExpectedBin(expr.Token(), lhs, rhs, op)
		}

		t.latestReturnedType = token.DDPBoolType()
	case token.GRÖßERODER, token.KLEINER, token.KLEINERODER, token.GRÖßER:
		validate(op, token.DDPIntType(), token.DDPFloatType())
		t.latestReturnedType = token.DDPBoolType()
	case token.LOGISCHODER, token.LOGISCHUND, token.KONTRA:
		validate(op, token.DDPIntType())
		t.latestReturnedType = token.DDPIntType()
	default:
		t.err(expr.Operator, "Unbekannter binärer Operator '%s'", expr.Operator)
	}
}
func (t *Typechecker) VisitTernaryExpr(expr *ast.TernaryExpr) {
	lhs := t.Evaluate(expr.Lhs)
	mid := t.Evaluate(expr.Mid)
	rhs := t.Evaluate(expr.Rhs)

	// helper to validate if types match
	validateBin := func(op token.TokenType, valid ...token.DDPType) {
		if !isOfTypeBin(mid, rhs, valid...) {
			t.errExpectedTern(expr.Token(), lhs, mid, rhs, op)
		}
	}

	switch expr.Operator.Type {
	case token.VONBIS:
		if !lhs.IsList && lhs != token.DDPStringType() {
			t.err(expr.Lhs.Token(), "Der VON_BIS Operator erwartet einen Text oder eine Liste als ersten Operanden, nicht %s", lhs)
		}

		validateBin(expr.Operator.Type, token.DDPIntType())
		if lhs.IsList {
			t.latestReturnedType = token.NewListType(lhs.PrimitiveType)
		} else if lhs == token.DDPStringType() {
			t.latestReturnedType = token.DDPStringType()
		}
	default:
		t.err(expr.Operator, "Unbekannter ternärer Operator '%s'", expr.Operator)
	}
}
func (t *Typechecker) VisitCastExpr(expr *ast.CastExpr) {
	lhs := t.Evaluate(expr.Lhs)
	if expr.Type.IsList {
		switch expr.Type.PrimitiveType {
		case token.ZAHL:
			if !isOfType(lhs, token.DDPIntType()) {
				t.err(expr.Token(), "Ein Ausdruck vom Typ %s kann nicht zu einer Zahlen Liste umgewandelt werden", lhs)
			}
		case token.KOMMAZAHL:
			if !isOfType(lhs, token.DDPFloatType()) {
				t.err(expr.Token(), "Ein Ausdruck vom Typ %s kann nicht zu einer Kommazahlen Liste umgewandelt werden", lhs)
			}
		case token.BOOLEAN:
			if !isOfType(lhs, token.DDPBoolType()) {
				t.err(expr.Token(), "Ein Ausdruck vom Typ %s kann nicht zu einer Boolean Liste umgewandelt werden", lhs)
			}
		case token.BUCHSTABE:
			if !isOfType(lhs, token.DDPCharType(), token.DDPStringType()) {
				t.err(expr.Token(), "Ein Ausdruck vom Typ %s kann nicht zu einer Buchstaben Liste umgewandelt werden", lhs)
			}
		case token.TEXT:
			if !isOfType(lhs, token.DDPStringType()) {
				t.err(expr.Token(), "Ein Ausdruck vom Typ %s kann nicht zu einer Text Liste umgewandelt werden", lhs)
			}
		default:
			t.err(expr.Token(), "Invalide Typumwandlung von %s zu %s", lhs, expr.Type)
		}
	} else {
		switch expr.Type.PrimitiveType {
		case token.ZAHL:
			if !lhs.IsPrimitive() {
				t.err(expr.Token(), "Eine %s kann nicht zu einer Zahl umgewandelt werden", lhs)
			}
		case token.KOMMAZAHL:
			if !lhs.IsPrimitive() {
				t.err(expr.Token(), "Eine %s kann nicht zu einer Kommazahl umgewandelt werden", lhs)
			}
			if !isOfType(lhs, token.DDPStringType(), token.DDPIntType(), token.DDPFloatType()) {
				t.err(expr.Token(), "Ein Ausdruck vom Typ %s kann nicht zu einer Kommazahl umgewandelt werden", lhs)
			}
		case token.BOOLEAN:
			if !lhs.IsPrimitive() {
				t.err(expr.Token(), "Eine %s kann nicht zu einem Boolean umgewandelt werden", lhs)
			}
			if !isOfType(lhs, token.DDPIntType(), token.DDPBoolType()) {
				t.err(expr.Token(), "Ein Ausdruck vom Typ %s kann nicht zu einem Boolean umgewandelt werden", lhs)
			}
		case token.BUCHSTABE:
			if !lhs.IsPrimitive() {
				t.err(expr.Token(), "Eine %s kann nicht zu einem Buchstaben umgewandelt werden", lhs)
			}
			if !isOfType(lhs, token.DDPIntType(), token.DDPCharType()) {
				t.err(expr.Token(), "Ein Ausdruck vom Typ %s kann nicht zu einem Buchstaben umgewandelt werden", lhs)
			}
		case token.TEXT:
		default:
			t.err(expr.Token(), "Invalide Typumwandlung von %s zu %s", lhs, expr.Type)
		}
	}
	t.latestReturnedType = expr.Type
}
func (t *Typechecker) VisitGrouping(expr *ast.Grouping) {
	expr.Expr.Accept(t)
}
func (t *Typechecker) VisitFuncCall(callExpr *ast.FuncCall) {
	for k, expr := range callExpr.Args {
		tokenType := t.Evaluate(expr)

		var argType token.ArgType
		decl, _ := t.CurrentTable.LookupFunc(callExpr.Name)

		for i, name := range decl.ParamNames {
			if name.Literal == k {
				argType = decl.ParamTypes[i]
				break
			}
		}

		if ass, ok := expr.(ast.Assigneable); argType.IsReference && !ok {
			t.err(expr.Token(), "Es wurde ein Referenz-Typ erwartet aber ein Ausdruck gefunden")
		} else if ass, ok := ass.(*ast.Indexing); argType.IsReference && argType.Type == token.DDPCharType() && ok {
			lhs := t.Evaluate(ass.Lhs)
			if lhs.PrimitiveType == token.TEXT {
				t.err(expr.Token(), "Ein Buchstabe in einem Text kann nicht als Buchstaben Referenz übergeben werden")
			}
		}
		if tokenType != argType.Type {
			t.err(expr.Token(),
				"Die Funktion %s erwartet einen Wert vom Typ %s für den Parameter %s, aber hat %s bekommen",
				callExpr.Name,
				argType,
				k,
				tokenType,
			)
		}
	}
	fun, _ := t.CurrentTable.LookupFunc(callExpr.Name)
	t.latestReturnedType = fun.Type
}

func (t *Typechecker) VisitBadStmt(stmt *ast.BadStmt) {
	t.Errored = true
	t.latestReturnedType = token.DDPVoidType()
}
func (t *Typechecker) VisitDeclStmt(stmt *ast.DeclStmt) {
	stmt.Decl.Accept(t)
}
func (t *Typechecker) VisitExprStmt(stmt *ast.ExprStmt) {
	stmt.Expr.Accept(t)
}
func (t *Typechecker) VisitAssignStmt(stmt *ast.AssignStmt) {
	rhs := t.Evaluate(stmt.Rhs)
	switch assign := stmt.Var.(type) {
	case *ast.Ident:
		if decl, exists := t.CurrentTable.LookupVar(assign.Literal.Literal); exists && decl.Type != rhs {
			t.err(stmt.Rhs.Token(),
				"Ein Wert vom Typ %s kann keiner Variable vom Typ %s zugewiesen werden",
				rhs,
				decl.Type,
			)
		}
	case *ast.Indexing:
		if typ := t.Evaluate(assign.Index); typ != token.DDPIntType() {
			t.err(assign.Index.Token(), "Der STELLE Operator erwartet eine Zahl als zweiten Operanden, nicht %s", typ)
		}

		lhs := t.Evaluate(assign.Lhs)
		if !lhs.IsList && lhs != token.DDPStringType() {
			t.err(assign.Lhs.Token(), "Der STELLE Operator erwartet einen Text oder eine Liste als ersten Operanden, nicht %s", lhs)
		}
		if lhs.IsList {
			lhs = token.NewPrimitiveType(lhs.PrimitiveType)
		} else if lhs == token.DDPStringType() {
			lhs = token.DDPCharType()
		}

		if lhs != rhs {
			t.err(stmt.Rhs.Token(),
				"Ein Wert vom Typ %s kann keiner Variable vom Typ %s zugewiesen werden",
				rhs,
				lhs,
			)
		}
	}
}
func (t *Typechecker) VisitBlockStmt(stmt *ast.BlockStmt) {
	if t.CheckBlocks {
		t.CurrentTable = stmt.Symbols
		for _, stmt := range stmt.Statements {
			t.visit(stmt)
		}

		t.CurrentTable = stmt.Symbols.Enclosing
	}
}
func (t *Typechecker) VisitIfStmt(stmt *ast.IfStmt) {
	conditionType := t.Evaluate(stmt.Condition)
	if conditionType != token.DDPBoolType() {
		t.err(stmt.Condition.Token(),
			"Die Bedingung einer WENN Anweisung muss vom Typ Boolean sein, war aber vom Typ %s",
			conditionType,
		)
	}
	t.visit(stmt.Then)
	if stmt.Else != nil {
		t.visit(stmt.Else)
	}
}
func (t *Typechecker) VisitWhileStmt(stmt *ast.WhileStmt) {
	conditionType := t.Evaluate(stmt.Condition)
	switch stmt.While.Type {
	case token.SOLANGE, token.MACHE:
		if conditionType != token.DDPBoolType() {
			t.err(stmt.Condition.Token(),
				"Die Bedingung einer SOLANGE Anweisung muss vom Typ BOOLEAN sein, war aber vom Typ %s",
				conditionType,
			)
		}
	case token.MAL:
		if conditionType != token.DDPIntType() {
			t.err(stmt.Condition.Token(),
				"Die Anzahl an Wiederholungen einer WIEDERHOLE Anweisung muss vom Typ ZAHL sein, war aber vom Typ %s",
				conditionType,
			)
		}
	}
	stmt.Body.Accept(t)
}
func (t *Typechecker) VisitForStmt(stmt *ast.ForStmt) {
	t.visit(stmt.Initializer)
	toType := t.Evaluate(stmt.To)
	if toType != token.DDPIntType() {
		t.err(stmt.To.Token(),
			"Es wurde ein Ausdruck vom Typ ZAHL erwartet aber %s gefunden",
			toType,
		)
	}
	if stmt.StepSize != nil {
		stepType := t.Evaluate(stmt.StepSize)
		if stepType != token.DDPIntType() {
			t.err(stmt.To.Token(),
				"Es wurde ein Ausdruck vom Typ ZAHL erwartet aber %s gefunden",
				stepType,
			)
		}
	}
	stmt.Body.Accept(t)
}
func (t *Typechecker) VisitForRangeStmt(stmt *ast.ForRangeStmt) {
	elementType := stmt.Initializer.Type
	inType := t.Evaluate(stmt.In)

	if !inType.IsList && inType != token.DDPStringType() {
		t.err(stmt.In.Token(), "Man kann nur über Texte oder Listen iterieren")
	}

	if inType.IsList && elementType != token.NewPrimitiveType(inType.PrimitiveType) {
		t.err(stmt.Initializer.Token(),
			"Es wurde ein Ausdruck vom Typ %s erwartet aber %s gefunden",
			token.NewListType(elementType.PrimitiveType), inType,
		)
	} else if inType == token.DDPStringType() && elementType != token.DDPCharType() {
		t.err(stmt.Initializer.Token(),
			"Es wurde ein Ausdruck vom Typ Buchstabe erwartet aber %s gefunden",
			elementType,
		)
	}
	stmt.Body.Accept(t)
}
func (t *Typechecker) VisitReturnStmt(stmt *ast.ReturnStmt) {
	returnType := token.DDPVoidType()
	if stmt.Value != nil {
		returnType = t.Evaluate(stmt.Value)
	}
	if fun, exists := t.CurrentTable.LookupFunc(stmt.Func); exists && fun.Type != returnType {
		t.err(stmt.Token(),
			"Eine Funktion mit Rückgabetyp %s kann keinen Wert vom Typ %s zurückgeben",
			fun.Type,
			returnType,
		)
	}
}

// checks if t is contained in types
func isOfType(t token.DDPType, types ...token.DDPType) bool {
	for _, v := range types {
		if t == v {
			return true
		}
	}
	return false
}

// checks if t1 and t2 are both contained in types
func isOfTypeBin(t1, t2 token.DDPType, types ...token.DDPType) bool {
	return isOfType(t1, types...) && isOfType(t2, types...)
}
