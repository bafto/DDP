package ast

import (
	"github.com/DDP-Projekt/Kompilierer/src/ddperror"
	"github.com/DDP-Projekt/Kompilierer/src/ddptypes"
	"github.com/DDP-Projekt/Kompilierer/src/token"
)

type (
	BadExpr struct {
		Tok token.Token
		Err ddperror.Error
	}

	Ident struct {
		Literal token.Token
		// the variable declaration this identifier refers to
		// is set by the resolver, or nil if the name was not found
		Declaration *VarDecl
	}

	// also exists as Binary expression for Literals
	// this one can count as Reference, and may be used
	// inplace of Ident (may be assigned to etc.)
	Indexing struct {
		Lhs   Assigneable // variable Name or other indexing
		Index Expression
	}

	// also exists as Binary expression for Literals
	// this one can count as Reference, and my be used
	// inplace of Ident (may be assigned to etc.)
	FieldAccess struct {
		Rhs   Assigneable // variable Name or other indexing
		Field *Ident      // the field name
	}

	IntLit struct {
		Literal token.Token
		Value   int64
	}

	FloatLit struct {
		Literal token.Token
		Value   float64 // the parsed float
	}

	BoolLit struct {
		Literal token.Token
		Value   bool
	}

	CharLit struct {
		Literal token.Token
		Value   rune
	}

	StringLit struct {
		Literal token.Token
		Value   string // the evaluated string
	}

	ListLit struct {
		Tok   token.Token
		Range token.Range
		// type of the empty list if Values is nil
		// the typechecker fills this field if Values is non-nil
		Type   ddptypes.ListType
		Values []Expression // the values in the Literal
		// if Values, Count and Value are nil, the list is empty
		Count Expression // for big list initializations
		Value Expression // the default value for big list initializations
	}

	UnaryExpr struct {
		Range    token.Range
		Tok      token.Token
		Operator UnaryOperator
		Rhs      Expression
	}

	BinaryExpr struct {
		Range    token.Range
		Tok      token.Token
		Lhs      Expression
		Operator BinaryOperator
		Rhs      Expression
	}

	// currently only used for von bis
	TernaryExpr struct {
		Range    token.Range
		Tok      token.Token
		Lhs      Expression
		Mid      Expression
		Rhs      Expression
		Operator TernaryOperator
	}

	// als Expressions cannot be unary
	// because the type operator might be multiple
	// tokens long
	CastExpr struct {
		Range token.Range
		Type  ddptypes.Type
		Lhs   Expression
	}

	// expressions that operate on types (Standardwert, Größe)
	TypeOpExpr struct {
		Range    token.Range
		Tok      token.Token
		Operator TypeOperator
		Rhs      ddptypes.Type
	}

	Grouping struct {
		Range  token.Range
		LParen token.Token // (
		Expr   Expression
	}

	FuncCall struct {
		Range token.Range
		Tok   token.Token // first token of the call
		Name  string      // name of the function
		// the function declaration this call refers to
		// is set by the parser, or nil if the name was not found
		Func *FuncDecl
		Args map[string]Expression
	}

	StructLiteral struct {
		Range token.Range
		Tok   token.Token // first token of the literal
		// the struct declaration this literal refers to
		// is set by the parser, or nil if the name was not found
		Struct *StructDecl
		// the arguments passed to the literal
		// this does not include all struct fields,
		// only the ones needed by the alias used
		Args map[string]Expression
	}

	// invokation of the alias of a ExpressionDecl
	ExpressionCall struct {
		Range         token.Range
		Tok           token.Token     // first token of the call
		Decl          *ExpressionDecl // the expression declaration this call refers to
		Args          map[string]Expression
		FilledSymbols *SymbolTable // copy of Decl.Symbols but with the variable types filled in properly
		Expr          Expression   // the reparsed expression (nil, if Decl.Expr != nil)
	}
)

func (expr *BadExpr) String() string        { return "BadExpr" }
func (expr *Ident) String() string          { return "Ident" }
func (expr *Indexing) String() string       { return "Indexing" }
func (expr *FieldAccess) String() string    { return "FieldAccess" }
func (expr *IntLit) String() string         { return "IntLit" }
func (expr *FloatLit) String() string       { return "FloatLit" }
func (expr *BoolLit) String() string        { return "BoolLit" }
func (expr *CharLit) String() string        { return "CharLit" }
func (expr *StringLit) String() string      { return "StringLit" }
func (expr *ListLit) String() string        { return "ListLit" }
func (expr *UnaryExpr) String() string      { return "UnaryExpr" }
func (expr *BinaryExpr) String() string     { return "BinaryExpr" }
func (expr *TernaryExpr) String() string    { return "BinaryExpr" }
func (expr *CastExpr) String() string       { return "CastExpr" }
func (expr *TypeOpExpr) String() string     { return "TypeOpExpr" }
func (expr *Grouping) String() string       { return "Grouping" }
func (expr *FuncCall) String() string       { return "FuncCall" }
func (expr *StructLiteral) String() string  { return "StructLiteral" }
func (expr *ExpressionCall) String() string { return "ExpressionCall" }

func (expr *BadExpr) Token() token.Token        { return expr.Tok }
func (expr *Ident) Token() token.Token          { return expr.Literal }
func (expr *Indexing) Token() token.Token       { return expr.Lhs.Token() }
func (expr *FieldAccess) Token() token.Token    { return expr.Field.Token() }
func (expr *IntLit) Token() token.Token         { return expr.Literal }
func (expr *FloatLit) Token() token.Token       { return expr.Literal }
func (expr *BoolLit) Token() token.Token        { return expr.Literal }
func (expr *CharLit) Token() token.Token        { return expr.Literal }
func (expr *StringLit) Token() token.Token      { return expr.Literal }
func (expr *ListLit) Token() token.Token        { return expr.Tok }
func (expr *UnaryExpr) Token() token.Token      { return expr.Tok }
func (expr *BinaryExpr) Token() token.Token     { return expr.Tok }
func (expr *TernaryExpr) Token() token.Token    { return expr.Tok }
func (expr *CastExpr) Token() token.Token       { return expr.Lhs.Token() }
func (expr *TypeOpExpr) Token() token.Token     { return expr.Tok }
func (expr *Grouping) Token() token.Token       { return expr.LParen }
func (expr *FuncCall) Token() token.Token       { return expr.Tok }
func (expr *StructLiteral) Token() token.Token  { return expr.Tok }
func (expr *ExpressionCall) Token() token.Token { return expr.Tok }

func (expr *BadExpr) GetRange() token.Range { return expr.Err.Range }
func (expr *Ident) GetRange() token.Range   { return token.NewRange(&expr.Literal, &expr.Literal) }
func (expr *Indexing) GetRange() token.Range {
	return token.Range{Start: expr.Lhs.GetRange().Start, End: expr.Index.GetRange().End}
}

func (expr *FieldAccess) GetRange() token.Range {
	return token.Range{Start: expr.Field.GetRange().Start, End: expr.Rhs.GetRange().End}
}
func (expr *IntLit) GetRange() token.Range         { return expr.Literal.Range }
func (expr *FloatLit) GetRange() token.Range       { return expr.Literal.Range }
func (expr *BoolLit) GetRange() token.Range        { return expr.Literal.Range }
func (expr *CharLit) GetRange() token.Range        { return expr.Literal.Range }
func (expr *StringLit) GetRange() token.Range      { return expr.Literal.Range }
func (expr *ListLit) GetRange() token.Range        { return expr.Range }
func (expr *UnaryExpr) GetRange() token.Range      { return expr.Range }
func (expr *BinaryExpr) GetRange() token.Range     { return expr.Range }
func (expr *TernaryExpr) GetRange() token.Range    { return expr.Range }
func (expr *CastExpr) GetRange() token.Range       { return expr.Range }
func (expr *TypeOpExpr) GetRange() token.Range     { return expr.Range }
func (expr *Grouping) GetRange() token.Range       { return expr.Range }
func (expr *FuncCall) GetRange() token.Range       { return expr.Range }
func (expr *StructLiteral) GetRange() token.Range  { return expr.Range }
func (expr *ExpressionCall) GetRange() token.Range { return expr.Range }

func (expr *BadExpr) Accept(v FullVisitor) VisitResult        { return v.VisitBadExpr(expr) }
func (expr *Ident) Accept(v FullVisitor) VisitResult          { return v.VisitIdent(expr) }
func (expr *Indexing) Accept(v FullVisitor) VisitResult       { return v.VisitIndexing(expr) }
func (expr *FieldAccess) Accept(v FullVisitor) VisitResult    { return v.VisitFieldAccess(expr) }
func (expr *IntLit) Accept(v FullVisitor) VisitResult         { return v.VisitIntLit(expr) }
func (expr *FloatLit) Accept(v FullVisitor) VisitResult       { return v.VisitFloatLit(expr) }
func (expr *BoolLit) Accept(v FullVisitor) VisitResult        { return v.VisitBoolLit(expr) }
func (expr *CharLit) Accept(v FullVisitor) VisitResult        { return v.VisitCharLit(expr) }
func (expr *StringLit) Accept(v FullVisitor) VisitResult      { return v.VisitStringLit(expr) }
func (expr *ListLit) Accept(v FullVisitor) VisitResult        { return v.VisitListLit(expr) }
func (expr *UnaryExpr) Accept(v FullVisitor) VisitResult      { return v.VisitUnaryExpr(expr) }
func (expr *BinaryExpr) Accept(v FullVisitor) VisitResult     { return v.VisitBinaryExpr(expr) }
func (expr *TernaryExpr) Accept(v FullVisitor) VisitResult    { return v.VisitTernaryExpr(expr) }
func (expr *CastExpr) Accept(v FullVisitor) VisitResult       { return v.VisitCastExpr(expr) }
func (expr *TypeOpExpr) Accept(v FullVisitor) VisitResult     { return v.VisitTypeOpExpr(expr) }
func (expr *Grouping) Accept(v FullVisitor) VisitResult       { return v.VisitGrouping(expr) }
func (expr *FuncCall) Accept(v FullVisitor) VisitResult       { return v.VisitFuncCall(expr) }
func (expr *StructLiteral) Accept(v FullVisitor) VisitResult  { return v.VisitStructLiteral(expr) }
func (expr *ExpressionCall) Accept(v FullVisitor) VisitResult { return v.VisitExpressionCall(expr) }

func (expr *BadExpr) expressionNode()        {}
func (expr *Ident) expressionNode()          {}
func (expr *Indexing) expressionNode()       {}
func (expr *FieldAccess) expressionNode()    {}
func (expr *IntLit) expressionNode()         {}
func (expr *FloatLit) expressionNode()       {}
func (expr *BoolLit) expressionNode()        {}
func (expr *CharLit) expressionNode()        {}
func (expr *StringLit) expressionNode()      {}
func (expr *ListLit) expressionNode()        {}
func (expr *UnaryExpr) expressionNode()      {}
func (expr *BinaryExpr) expressionNode()     {}
func (expr *TernaryExpr) expressionNode()    {}
func (expr *CastExpr) expressionNode()       {}
func (expr *TypeOpExpr) expressionNode()     {}
func (expr *Grouping) expressionNode()       {}
func (expr *FuncCall) expressionNode()       {}
func (expr *StructLiteral) expressionNode()  {}
func (expr *ExpressionCall) expressionNode() {}

func (expr *Ident) assigneable()       {}
func (expr *Indexing) assigneable()    {}
func (expr *FieldAccess) assigneable() {}
