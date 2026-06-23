package ast

type Type string

const (
	Any         Type = "any"
	Integer     Type = "integer"
	Number      Type = "number"
	String      Type = "string"
	Ident       Type = "ident"
	Object      Type = "object"
	Array       Type = "array"
	MultiArray  Type = "multiarray"
	Function    Type = "function"
	FunctionObj Type = "functionobj"
)

type Node interface{ node() }
type Stmt interface {
	Node
	stmt()
}
type Expr interface {
	Stmt
	expr()
	Type() Type
	Text() string
	SetAssign(bool)
	IsAssign() bool
}
type BaseExpr struct{ Assign bool }

func (BaseExpr) node()               {}
func (BaseExpr) stmt()               {}
func (BaseExpr) expr()               {}
func (b *BaseExpr) SetAssign(v bool) { b.Assign = v }
func (b *BaseExpr) IsAssign() bool   { return b.Assign }

type Block struct{ Stmts []Stmt }

func (*Block) node() {}
func (*Block) stmt() {}

type If struct {
	Cond       Expr
	Then, Else Stmt
}

func (*If) node() {}
func (*If) stmt() {}

type FnDecl struct {
	Public       bool
	Object, Name string
	Args         []Expr
	Body         *Block
	EmitPrejump  bool
}

func (*FnDecl) node() {}
func (*FnDecl) stmt() {}

type NewStmt struct {
	Name string
	Args []Expr
	Body *Block
}

func (*NewStmt) node() {}
func (*NewStmt) stmt() {}

type Break struct{}

func (*Break) node() {}
func (*Break) stmt() {}

type Continue struct{}

func (*Continue) node() {}
func (*Continue) stmt() {}

type Return struct{ Value Expr }

func (*Return) node() {}
func (*Return) stmt() {}

type While struct {
	Cond Expr
	Body Stmt
}

func (*While) node() {}
func (*While) stmt() {}

type DoWhile struct {
	Cond Expr
	Body Stmt
}

func (*DoWhile) node() {}
func (*DoWhile) stmt() {}

type With struct {
	Target Expr
	Body   Stmt
}

func (*With) node() {}
func (*With) stmt() {}

type For struct {
	Init, Cond, Post Expr
	Body             Stmt
}

func (*For) node() {}
func (*For) stmt() {}

type ForEach struct {
	Name, Range Expr
	Body        Stmt
}

func (*ForEach) node() {}
func (*ForEach) stmt() {}

type Switch struct {
	Target Expr
	Cases  []SwitchCase
}

func (*Switch) node() {}
func (*Switch) stmt() {}

type SwitchCase struct {
	Exprs []Expr
	Body  *Block
}

type IntLit struct {
	BaseExpr
	Value int
}

func (n *IntLit) Type() Type   { return Integer }
func (n *IntLit) Text() string { return itoa(n.Value) }

type FloatLit struct {
	BaseExpr
	Value string
}

func (n *FloatLit) Type() Type   { return Number }
func (n *FloatLit) Text() string { return n.Value }

type StringLit struct {
	BaseExpr
	Value string
}

func (n *StringLit) Type() Type   { return String }
func (n *StringLit) Text() string { return n.Value }

type ConstLit struct {
	BaseExpr
	Value string
}

func (n *ConstLit) Type() Type   { return Ident }
func (n *ConstLit) Text() string { return n.Value }

type Identifier struct {
	BaseExpr
	Name          string
	CheckReserved bool
}

func (n *Identifier) Type() Type   { return Ident }
func (n *Identifier) Text() string { return n.Name }

type Postfix struct {
	BaseExpr
	Nodes []Expr
}

func (n *Postfix) Type() Type {
	if len(n.Nodes) == 0 {
		return Any
	}
	return n.Nodes[len(n.Nodes)-1].Type()
}
func (n *Postfix) Text() string {
	s := ""
	for i, e := range n.Nodes {
		if i > 0 {
			s += "."
		}
		s += e.Text()
	}
	return s
}
func (n *Postfix) SetAssign(v bool) {
	n.Assign = v
	if len(n.Nodes) > 0 {
		n.Nodes[len(n.Nodes)-1].SetAssign(v)
	}
}

type ArrayIndex struct {
	BaseExpr
	Exprs []Expr
}

func (n *ArrayIndex) Type() Type {
	if len(n.Exprs) > 1 {
		return MultiArray
	}
	return Array
}
func (n *ArrayIndex) Text() string { return "[]" }

type Cast struct {
	BaseExpr
	Kind  Type
	Value Expr
}

func (n *Cast) Type() Type   { return n.Kind }
func (n *Cast) Text() string { return n.Value.Text() }

type In struct {
	BaseExpr
	Value, Lower, Higher Expr
}

func (n *In) Type() Type   { return Integer }
func (n *In) Text() string { return "in" }

type Ternary struct {
	BaseExpr
	Cond, Left, Right Expr
}

func (n *Ternary) Type() Type {
	if n.Left.Type() == n.Right.Type() {
		return n.Left.Type()
	}
	return Any
}
func (n *Ternary) Text() string { return n.Left.Text() }

type Binary struct {
	BaseExpr
	Left, Right Expr
	Op          string
	Sep         byte
}

func (n *Binary) Type() Type {
	switch n.Op {
	case "=":
		return n.Right.Type()
	case "@", "@=":
		return String
	}
	switch n.Op {
	case "+", "-", "*", "/", "%", "^", "&", "|", "xor", "<<", ">>", "==", "!=", "<", "<=", ">", ">=", "&&", "||":
		return Number
	}
	return n.Left.Type()
}
func (n *Binary) Text() string { return n.Left.Text() + " " + n.Op + " " + n.Right.Text() }

type Unary struct {
	BaseExpr
	Op     string
	Value  Expr
	Prefix bool
	Unused bool
}

func (n *Unary) Type() Type {
	if n.Op == "-" || n.Op == "!" || n.Op == "~" {
		return Number
	}
	return n.Value.Type()
}
func (n *Unary) Text() string {
	if n.Prefix {
		return n.Op + n.Value.Text()
	}
	return n.Value.Text() + n.Op
}

type FnCall struct {
	BaseExpr
	Func, Object Expr
	Args         []Expr
}

func (n *FnCall) Type() Type   { return Function }
func (n *FnCall) Text() string { return n.Func.Text() + "()" }

type NewArray struct {
	BaseExpr
	Dims []Expr
}

func (n *NewArray) Type() Type   { return Array }
func (n *NewArray) Text() string { return "new[]" }

type NewObject struct {
	BaseExpr
	Class Expr
	Args  []Expr
}

func (n *NewObject) Type() Type   { return Object }
func (n *NewObject) Text() string { return "new " + n.Class.Text() }

type List struct {
	BaseExpr
	Args []Expr
}

func (n *List) Type() Type   { return Array }
func (n *List) Text() string { return "{}" }

type FnObject struct {
	BaseExpr
	Name string
	Args []Expr
	Body *Block
}

func (n *FnObject) Type() Type   { return FunctionObj }
func (n *FnObject) Text() string { return "() -> {}" }

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [32]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
