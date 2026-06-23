package compiler

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/MorenoLand/GScript.gs2parser-go/ast"
	"github.com/MorenoLand/GScript.gs2parser-go/bytecode"
	"github.com/MorenoLand/GScript.gs2parser-go/opcode"
)

type Compiler struct {
	bc                             *bytecode.Builder
	nextLabel                      uint32
	locs                           map[uint32][]int
	addrs                          map[uint32]uint32
	success, fail, exit, brk, cont uint32
	inside, inline                 bool
	inlineLogical                  bool
	copyAssign                     bool
	directBlock                    bool
	logicalParent                  string
	lastCallReturn                 bool
	negFloats                      map[string]int
	newObjectCount                 int
	fnCallLocals                   callLocals
	Joins                          map[string]bool
}

type callLocals struct {
	slots map[string]int32
	order []string
}

func Compile(root *ast.Block) ([]byte, error) {
	c := New()
	if err := c.Stmt(root); err != nil {
		return nil, err
	}
	c.set(c.exit, c.bc.OpIndex())
	c.writeLabels()
	return c.bc.Bytes(), nil
}

func New() *Compiler {
	c := &Compiler{bc: bytecode.New(), locs: map[uint32][]int{}, addrs: map[uint32]uint32{}, inline: true, negFloats: map[string]int{}, Joins: map[string]bool{}}
	c.exit = c.label()
	c.success = c.exit
	c.fail = c.exit
	return c
}
func (c *Compiler) label() uint32             { c.nextLabel++; return c.nextLabel }
func (c *Compiler) at(l uint32, pos int)      { c.locs[l] = append(c.locs[l], pos) }
func (c *Compiler) set(l uint32, addr uint32) { c.addrs[l] = addr }
func (c *Compiler) writeLabels() {
	for l, locs := range c.locs {
		if l == c.exit {
			continue
		}
		for _, pos := range locs {
			c.bc.Short(int16(c.addrs[l]), pos)
		}
	}
}
func (c *Compiler) jumpPlaceholder(l uint32) {
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	c.at(l, c.bc.Pos()-2)
}
func (c *Compiler) intPlaceholder() int {
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	return c.bc.Pos() - 2
}

func (c *Compiler) Stmt(s ast.Stmt) error {
	switch n := s.(type) {
	case nil:
	case *ast.Block:
		for _, x := range n.Stmts {
			direct := c.directBlock
			c.directBlock = true
			if err := c.Stmt(x); err != nil {
				c.directBlock = direct
				return err
			}
			c.directBlock = direct
		}
	case *ast.FnDecl:
		return c.fn(n)
	case *ast.If:
		return c.ifStmt(n)
	case *ast.While:
		return c.whileStmt(n)
	case *ast.DoWhile:
		return c.doWhileStmt(n)
	case *ast.For:
		return c.forStmt(n)
	case *ast.ForEach:
		return c.foreachStmt(n)
	case *ast.Switch:
		return c.switchStmt(n)
	case *ast.With:
		return c.withStmt(n)
	case *ast.NewStmt:
		return c.newStmt(n)
	case *ast.Break:
		if c.brk == 0 {
			return nil
		}
		c.bc.Op(opcode.SetIndex)
		c.jumpPlaceholder(c.brk)
	case *ast.Continue:
		if c.cont == 0 {
			return nil
		}
		c.bc.Op(opcode.SetIndex)
		c.jumpPlaceholder(c.cont)
	case *ast.Return:
		if n.Value != nil {
			if err := c.Expr(n.Value); err != nil {
				return err
			}
		} else {
			c.num(0)
		}
		c.bc.Op(opcode.Ret)
	case ast.Expr:
		c.lastCallReturn = false
		if err := c.Expr(n); err != nil {
			return err
		}
		_, topCall := n.(*ast.FnCall)
		if c.lastCallReturn && topCall {
			c.bc.Op(opcode.IndexDec)
			c.lastCallReturn = false
		}
	default:
		return fmt.Errorf("unhandled statement %T", s)
	}
	return nil
}

func (c *Compiler) fn(n *ast.FnDecl) error {
	prevCallLocals := c.fnCallLocals
	c.fnCallLocals = repeatedBareCallLocals(n.Body)
	defer func() { c.fnCallLocals = prevCallLocals }()
	jmpLoc := 0
	if n.EmitPrejump {
		c.bc.Op(opcode.SetIndex)
		c.intPlaceholder()
		jmpLoc = c.bc.Pos()
	}
	name := n.Name
	if n.Object != "" {
		name = n.Object + "." + name
		if fnObjectAlias(n) {
			name = n.Name + "," + name
		}
	}
	if n.Public {
		name = "public." + name
	}
	c.bc.AddFunction(name, c.bc.OpIndex(), jmpLoc)
	c.bc.Op(opcode.TypeArray)
	for i := len(n.Args) - 1; i >= 0; i-- {
		if err := c.Expr(n.Args[i]); err != nil {
			return err
		}
	}
	c.bc.Op(opcode.FuncParamsEnd)
	c.bc.Op(opcode.Jmp)
	if hasFunctionCall(n.Body) {
		c.bc.Op(opcode.CmdCall)
	}
	c.emitCallLocals()
	if err := c.Stmt(n.Body); err != nil {
		return err
	}
	if c.bc.LastOp() != opcode.Ret {
		c.num(0)
		c.bc.Op(opcode.Ret)
	}
	return nil
}

func fnObjectAlias(n *ast.FnDecl) bool {
	if n.Object == "" || n.Body == nil || len(n.Body.Stmts) != 1 {
		return false
	}
	ret, ok := n.Body.Stmts[0].(*ast.Return)
	if !ok {
		return false
	}
	call, ok := ret.Value.(*ast.FnCall)
	if !ok || call.Object != nil || len(call.Args) != 0 {
		return false
	}
	return call.Func.Text() == n.Name
}

func (c *Compiler) ifStmt(n *ast.If) error {
	os, of := c.success, c.fail
	direct := c.directBlock
	s, f := c.label(), c.label()
	c.success, c.fail = s, f
	c.inline = false
	if err := c.Expr(n.Cond); err != nil {
		return err
	}
	c.inline = true
	if !opcode.BooleanReturning(c.bc.LastOp()) {
		c.bc.Convert(string(n.Cond.Type()), string(ast.Number))
	}
	c.set(s, c.bc.OpIndex())
	c.bc.Op(opcode.If)
	c.jumpPlaceholder(f)
	c.directBlock = false
	if err := c.Stmt(n.Then); err != nil {
		c.directBlock = direct
		return err
	}
	c.directBlock = direct
	c.set(f, c.bc.OpIndex()+boolU32(n.Else != nil))
	c.success, c.fail = os, of
	if n.Else != nil {
		c.bc.Op(opcode.SetIndex)
		loc := c.intPlaceholder()
		c.directBlock = false
		if err := c.Stmt(n.Else); err != nil {
			c.directBlock = direct
			return err
		}
		c.directBlock = direct
		c.bc.Short(int16(c.bc.OpIndex()), loc)
	}
	return nil
}

func (c *Compiler) whileStmt(n *ast.While) error {
	os, of, ob, oc := c.success, c.fail, c.brk, c.cont
	direct := c.directBlock
	c.brk, c.cont = c.label(), c.label()
	c.set(c.cont, c.bc.OpIndex())
	c.inline = false
	if err := c.Expr(n.Cond); err != nil {
		return err
	}
	c.inline = true
	c.bc.Convert(string(n.Cond.Type()), string(ast.Number))
	c.bc.Op(opcode.If)
	c.jumpPlaceholder(c.brk)
	c.bc.Op(opcode.CmdCall)
	c.directBlock = false
	if err := c.Stmt(n.Body); err != nil {
		c.directBlock = direct
		return err
	}
	c.directBlock = direct
	c.bc.Op(opcode.SetIndex)
	c.jumpPlaceholder(c.cont)
	c.set(c.brk, c.bc.OpIndex())
	c.success, c.fail, c.brk, c.cont = os, of, ob, oc
	return nil
}

func (c *Compiler) doWhileStmt(n *ast.DoWhile) error {
	os, of, ob, oc := c.success, c.fail, c.brk, c.cont
	direct := c.directBlock
	start := c.bc.OpIndex()
	c.brk, c.cont = c.label(), c.label()
	c.bc.Op(opcode.CmdCall)
	c.directBlock = false
	if err := c.Stmt(n.Body); err != nil {
		c.directBlock = direct
		return err
	}
	c.directBlock = direct
	c.set(c.cont, c.bc.OpIndex())
	c.inline = false
	if err := c.Expr(n.Cond); err != nil {
		return err
	}
	c.inline = true
	c.bc.Convert(string(n.Cond.Type()), string(ast.Number))
	c.bc.Op(opcode.SetIndexTrue)
	c.bc.DynamicNumber(int32(start))
	c.set(c.brk, c.bc.OpIndex())
	c.success, c.fail, c.brk, c.cont = os, of, ob, oc
	return nil
}

func (c *Compiler) forStmt(n *ast.For) error {
	direct := c.directBlock
	if n.Init != nil {
		if err := c.Expr(n.Init); err != nil {
			return err
		}
	}
	start := c.bc.OpIndex()
	if n.Cond != nil {
		if err := c.Expr(n.Cond); err != nil {
			return err
		}
		c.bc.Convert(string(n.Cond.Type()), string(ast.Number))
	} else {
		c.bc.Op(opcode.TypeTrue)
	}
	ob, oc := c.brk, c.cont
	c.brk, c.cont = c.label(), c.label()
	c.bc.Op(opcode.If)
	c.jumpPlaceholder(c.brk)
	c.bc.Op(opcode.CmdCall)
	c.directBlock = false
	if err := c.Stmt(n.Body); err != nil {
		c.directBlock = direct
		return err
	}
	c.directBlock = direct
	c.set(c.cont, c.bc.OpIndex())
	if n.Post != nil {
		if err := c.Expr(n.Post); err != nil {
			return err
		}
	}
	c.bc.Op(opcode.SetIndex)
	c.bc.DynamicNumber(int32(start))
	c.set(c.brk, c.bc.OpIndex())
	c.brk, c.cont = ob, oc
	return nil
}

func (c *Compiler) foreachStmt(n *ast.ForEach) error {
	direct := c.directBlock
	c.Expr(n.Name)
	c.Expr(n.Range)
	c.bc.Op(opcode.ConvToObject)
	c.num(0)
	ob, oc := c.brk, c.cont
	c.brk, c.cont = c.label(), c.label()
	start := c.bc.OpIndex()
	c.bc.Op(opcode.ForEach)
	c.jumpPlaceholder(c.brk)
	c.bc.Op(opcode.CmdCall)
	c.directBlock = false
	if err := c.Stmt(n.Body); err != nil {
		c.directBlock = direct
		return err
	}
	c.directBlock = direct
	c.set(c.cont, c.bc.OpIndex())
	c.bc.Op(opcode.Inc)
	c.bc.Op(opcode.SetIndex)
	c.bc.DynamicNumber(int32(start))
	c.set(c.brk, c.bc.OpIndex())
	c.brk, c.cont = ob, oc
	c.bc.Op(opcode.IndexDec)
	c.bc.Op(opcode.IndexDec)
	c.bc.Op(opcode.IndexDec)
	return nil
}

func (c *Compiler) switchStmt(n *ast.Switch) error {
	ob, oc, os, of := c.brk, c.cont, c.success, c.fail
	c.brk = c.label()
	var caseLabels []uint32
	c.bc.Op(opcode.SetIndex)
	caseTestLoc := c.intPlaceholder()
	for _, cs := range n.Cases {
		lbl := c.label()
		c.set(lbl, c.bc.OpIndex())
		for range cs.Exprs {
			caseLabels = append(caseLabels, lbl)
		}
		c.cont = lbl
		if err := c.Stmt(cs.Body); err != nil {
			return err
		}
	}
	c.bc.Short(int16(c.bc.OpIndex()), caseTestLoc)
	if err := c.Expr(n.Target); err != nil {
		return err
	}
	i := 0
	for _, cs := range n.Cases {
		for j := len(cs.Exprs) - 1; j >= 0; j-- {
			ex := cs.Exprs[j]
			if ex != nil {
				c.bc.Op(opcode.CopyLastOps)
				if err := c.Expr(ex); err != nil {
					return err
				}
				c.bc.Op(opcode.Eq)
				c.bc.Op(opcode.SetIndexTrue)
			} else {
				c.bc.Op(opcode.SetIndex)
			}
			c.bc.DynamicNumber(int32(c.addrs[caseLabels[i]]))
			i++
		}
	}
	c.set(c.brk, c.bc.OpIndex())
	c.bc.Op(opcode.IndexDec)
	c.brk, c.cont, c.success, c.fail = ob, oc, os, of
	return nil
}

func (c *Compiler) withStmt(n *ast.With) error {
	if err := c.Expr(n.Target); err != nil {
		return err
	}
	c.bc.Op(opcode.ConvToObject)
	c.bc.Op(opcode.With)
	loc := c.intPlaceholder()
	if err := c.Stmt(n.Body); err != nil {
		return err
	}
	c.bc.Op(opcode.WithEnd)
	c.bc.Short(int16(c.bc.OpIndex()), loc)
	return nil
}

func (c *Compiler) newStmt(n *ast.NewStmt) error {
	for _, a := range n.Args {
		if err := c.Expr(a); err != nil {
			return err
		}
	}
	c.bc.Op(opcode.InlineNew)
	c.bc.Op(opcode.CopyLastOps)
	c.bc.Op(opcode.CopyLastOps)
	c.bc.Op(opcode.CopyLastOps)
	c.str(n.Name, opcode.TypeString)
	c.bc.Op(opcode.ConvToString)
	c.bc.Op(opcode.NewObject)
	c.bc.Op(opcode.Assign)
	c.bc.Op(opcode.ConvToObject)
	c.bc.Op(opcode.With)
	loc := c.intPlaceholder()
	prev := c.newObjectCount
	c.newObjectCount++
	if n.Body != nil {
		if err := c.Stmt(n.Body); err != nil {
			return err
		}
	}
	c.bc.Op(opcode.WithEnd)
	c.bc.Short(int16(c.bc.OpIndex()), loc)
	for i := 0; i < c.newObjectCount-prev; i++ {
		c.bc.Op(opcode.TypeArray)
		c.bc.Op(opcode.SwapLastOps)
		c.str("addcontrol", opcode.TypeVar)
		c.bc.Op(opcode.Call)
		c.bc.Op(opcode.IndexDec)
	}
	c.newObjectCount--
	return nil
}

func (c *Compiler) Expr(e ast.Expr) error {
	switch n := e.(type) {
	case *ast.IntLit:
		c.num(int32(n.Value))
	case *ast.ConstLit:
		v, _ := strconv.Atoi(n.Value)
		c.num(int32(v))
	case *ast.FloatLit:
		c.bc.Op(opcode.TypeNumber)
		c.bc.DoubleNumber(strings.Repeat("-", c.negFloats[n.Value]) + n.Value)
	case *ast.StringLit:
		c.str(n.Value, opcode.TypeString)
	case *ast.Identifier:
		c.ident(n)
	case *ast.Postfix:
		return c.postfix(n)
	case *ast.ArrayIndex:
		for _, x := range n.Exprs {
			c.Expr(x)
			c.bc.Convert(string(x.Type()), string(ast.Number))
		}
		if !n.IsAssign() {
			if n.Type() == ast.MultiArray {
				c.bc.Op(opcode.ArrayMultidim)
			} else {
				c.bc.Op(opcode.Array)
			}
		}
	case *ast.Cast:
		c.Expr(n.Value)
		switch n.Kind {
		case ast.Integer:
			c.bc.Convert(string(n.Value.Type()), string(ast.Number))
			c.bc.Op(opcode.Int)
		case ast.Number:
			c.bc.Op(opcode.ConvToFloat)
		case ast.String:
			c.bc.Convert(string(n.Value.Type()), string(ast.String))
			c.bc.Op(opcode.Translate)
		}
	case *ast.In:
		c.Expr(n.Value)
		c.Expr(n.Lower)
		if n.Higher != nil {
			c.bc.Convert(string(n.Lower.Type()), string(ast.Number))
			c.Expr(n.Higher)
			c.bc.Convert(string(n.Higher.Type()), string(ast.Number))
			c.bc.Op(opcode.InRange)
		} else {
			c.bc.Convert(string(n.Lower.Type()), string(ast.Object))
			c.bc.Op(opcode.InObj)
		}
	case *ast.Ternary:
		return c.ternary(n)
	case *ast.Binary:
		return c.binary(n)
	case *ast.Unary:
		return c.unary(n)
	case *ast.FnCall:
		return c.call(n)
	case *ast.NewArray:
		if len(n.Dims) == 0 {
			return nil
		}
		if err := c.Expr(n.Dims[0]); err != nil {
			return err
		}
		c.bc.Convert(string(n.Dims[0].Type()), string(ast.Number))
		c.bc.Op(opcode.ArrayNew)
		for _, d := range n.Dims[1:] {
			if err := c.Expr(d); err != nil {
				return err
			}
			c.bc.Convert(string(d.Type()), string(ast.Number))
			c.bc.Op(opcode.ArrayNewMultidim)
		}
	case *ast.NewObject:
		classID := c.bc.StringID(n.Class.Text())
		if len(n.Args) == 1 {
			c.Expr(n.Args[0])
			c.bc.Op(opcode.InlineNew)
		} else {
			c.str("unknown_object", opcode.TypeVar)
		}
		c.bc.Op(opcode.TypeString)
		c.bc.DynamicUnsigned(uint32(classID))
		c.bc.Op(opcode.NewObject)
	case *ast.List:
		c.bc.Op(opcode.TypeArray)
		for i := len(n.Args) - 1; i >= 0; i-- {
			c.Expr(n.Args[i])
		}
		c.bc.Op(opcode.ArrayEnd)
	case *ast.FnObject:
		c.bc.Op(opcode.SetIndex)
		loc := c.intPlaceholder()
		c.fn(&ast.FnDecl{Public: true, Name: n.Name, Args: n.Args, Body: n.Body})
		c.bc.Short(int16(c.bc.OpIndex()), loc)
		c.bc.Op(opcode.This)
		c.str(n.Name, opcode.TypeVar)
		c.bc.Op(opcode.MemberAccess)
		c.bc.Op(opcode.ConvToObject)
	default:
		return fmt.Errorf("unhandled expression %T", e)
	}
	return nil
}

func (c *Compiler) binary(n *ast.Binary) error {
	if n.Op == "&&" || n.Op == "||" {
		return c.logical(n)
	}
	if n.Op == "@" {
		c.Expr(n.Left)
		c.bc.Convert(string(n.Left.Type()), string(ast.String))
		if n.Sep != 0 {
			c.str(string([]byte{n.Sep}), opcode.TypeString)
			c.bc.Op(opcode.Join)
		}
		c.Expr(n.Right)
		c.bc.Convert(string(n.Right.Type()), string(ast.String))
		c.bc.Op(opcode.Join)
		return nil
	}
	if isAssignOp(n.Op) {
		c.Expr(n.Left)
		if n.Op != "=" {
			c.bc.Op(opcode.CopyLastOps)
			if n.Op == "@=" {
				c.bc.Convert(string(n.Left.Type()), string(ast.String))
				c.Expr(n.Right)
				c.bc.Convert(string(n.Right.Type()), string(ast.String))
				c.bc.Op(opcode.Join)
			} else {
				c.bc.Convert(string(n.Left.Type()), string(ast.Number))
				c.Expr(n.Right)
				c.bc.Convert(string(n.Right.Type()), string(ast.Number))
				c.bc.Op(opFor(n.Op))
			}
			op := opcode.Assign
			if n.Left.Type() == ast.Array {
				op = opcode.ArrayAssign
			} else if n.Left.Type() == ast.MultiArray {
				op = opcode.ArrayMultidimAssign
			}
			c.bc.Op(op)
			return nil
		}
		if c.copyAssign {
			c.bc.Op(opcode.CopyLastOps)
			c.copyAssign = false
		}
		if n.Right.IsAssign() {
			c.copyAssign = true
		}
		c.Expr(n.Right)
		op := opcode.Assign
		if n.Left.Type() == ast.Array {
			op = opcode.ArrayAssign
		} else if n.Left.Type() == ast.MultiArray {
			op = opcode.ArrayMultidimAssign
		}
		c.bc.Op(op)
		return nil
	}
	if v, ok := foldedNumber(n); ok {
		c.number(v)
		return nil
	}
	c.Expr(n.Left)
	if numericOp(n.Op) {
		c.convertNumeric(n.Left)
	}
	c.Expr(n.Right)
	if numericOp(n.Op) {
		c.convertNumeric(n.Right)
	}
	c.bc.Op(opFor(n.Op))
	return nil
}

func (c *Compiler) convertNumeric(e ast.Expr) {
	if _, ok := e.(*ast.Ternary); ok {
		c.bc.Op(opcode.ConvToFloat)
		return
	}
	c.bc.Convert(string(e.Type()), string(ast.Number))
}

func (c *Compiler) unary(n *ast.Unary) error {
	if n.Op == "-" {
		if lit, ok := n.Value.(*ast.IntLit); ok {
			c.num(int32(-lit.Value))
			return nil
		}
		if lit, ok := n.Value.(*ast.FloatLit); ok {
			c.bc.Op(opcode.TypeNumber)
			c.negFloats[lit.Value]++
			c.bc.DoubleNumber(strings.Repeat("-", c.negFloats[lit.Value]) + lit.Value)
			return nil
		}
	}
	first := !c.inside
	inline, inlineLogical := c.inline, c.inlineLogical
	os, of := c.success, c.fail
	var label uint32
	if first {
		c.inside = true
		c.inline = true
		c.inlineLogical = true
		label = c.label()
		c.success, c.fail = label, label
	}
	c.Expr(n.Value)
	if first {
		c.inside = false
		c.inline = inline
		c.inlineLogical = inlineLogical
		c.success, c.fail = os, of
		c.set(label, c.bc.OpIndex())
	}
	if (n.Op == "++" || n.Op == "--") && !n.Prefix {
		c.bc.Op(opcode.CopyLastOps)
		c.bc.Op(opcode.ConvToFloat)
		c.bc.Op(opcode.SwapLastOps)
		if n.Op == "++" {
			c.bc.Op(opcode.Inc)
		} else {
			c.bc.Op(opcode.Dec)
		}
		c.bc.Op(opcode.IndexDec)
		return nil
	}
	switch n.Op {
	case "++":
		c.bc.Op(opcode.Inc)
		if n.Unused {
			c.bc.Op(opcode.IndexDec)
		}
	case "--":
		c.bc.Op(opcode.Dec)
		if n.Unused {
			c.bc.Op(opcode.IndexDec)
		}
	case "-":
		c.bc.Convert(string(n.Value.Type()), string(ast.Number))
		c.bc.Op(opcode.UnarySub)
	case "!":
		c.bc.Convert(string(n.Value.Type()), string(ast.Number))
		c.bc.Op(opcode.Not)
	case "~":
		c.bc.Convert(string(n.Value.Type()), string(ast.Number))
		c.bc.Op(opcode.Bwi)
	case "@":
		c.bc.Op(opcode.ConvToString)
		if n.Value.Type() == ast.Array {
			c.bc.Op(opcode.MemberAccess)
		}
	}
	return nil
}

func (c *Compiler) ternary(n *ast.Ternary) error {
	c.Expr(n.Cond)
	if !opcode.BooleanReturning(c.bc.LastOp()) {
		c.bc.Convert(string(n.Cond.Type()), string(ast.Number))
	}
	fail, succ := c.label(), c.label()
	c.bc.Op(opcode.If)
	c.jumpPlaceholder(fail)
	c.Expr(n.Left)
	c.set(fail, c.bc.OpIndex()+1)
	c.bc.Op(opcode.SetIndex)
	c.jumpPlaceholder(succ)
	c.Expr(n.Right)
	c.set(succ, c.bc.OpIndex())
	return nil
}

func (c *Compiler) logical(n *ast.Binary) error {
	first := !c.inside
	saveFail, saveSuccess := c.fail, c.success
	if first {
		c.inside = true
		if c.inline {
			l := c.label()
			c.success, c.fail = l, l
		}
	}
	tmpSuccess, tmpFail := c.success, c.fail
	inline := c.inline
	if n.Op == "&&" {
		nextSuccess := c.label()
		c.success = nextSuccess
		if err := c.Expr(n.Left); err != nil {
			return err
		}
		c.bc.Convert(string(n.Left.Type()), string(ast.Number))
		c.set(nextSuccess, c.bc.OpIndex())
		c.success, c.fail = tmpSuccess, tmpFail
		if inline {
			c.bc.Op(opcode.And)
		} else {
			c.bc.Op(opcode.If)
		}
		c.jumpPlaceholder(c.fail)
		if err := c.Expr(n.Right); err != nil {
			return err
		}
		c.bc.Convert(string(n.Right.Type()), string(ast.Number))
	} else if n.Op == "||" {
		nextFail := c.label()
		c.fail = nextFail
		if err := c.Expr(n.Left); err != nil {
			return err
		}
		c.bc.Convert(string(n.Left.Type()), string(ast.Number))
		c.bc.Op(opcode.Or)
		c.jumpPlaceholder(c.success)
		c.set(nextFail, c.bc.OpIndex())
		c.success, c.fail = tmpSuccess, tmpFail
		if err := c.Expr(n.Right); err != nil {
			return err
		}
		c.bc.Convert(string(n.Right.Type()), string(ast.Number))
	}
	if first {
		c.set(tmpSuccess, c.bc.OpIndex())
		c.fail, c.success = saveFail, saveSuccess
		c.inside = false
		if inline {
			c.bc.Op(opcode.InlineConditional)
		}
	} else {
		c.success, c.fail = tmpSuccess, tmpFail
	}
	return nil
}

func hasLogicalOp(e ast.Expr, op string) bool {
	n, ok := e.(*ast.Binary)
	if !ok {
		return false
	}
	return n.Op == op || hasLogicalOp(n.Left, op) || hasLogicalOp(n.Right, op)
}

func (c *Compiler) postfix(n *ast.Postfix) error {
	for i, node := range n.Nodes {
		dynamic := false
		if i > 0 {
			if u, ok := node.(*ast.Unary); ok && u.Op == "@" {
				if dynamicMemberAdd(u.Value) {
					if err := c.Expr(u.Value); err != nil {
						return err
					}
					c.bc.Convert(string(u.Value.Type()), string(ast.Number))
					c.bc.Op(opcode.Add)
					dynamic = true
				}
			}
		}
		if !dynamic {
			if err := c.Expr(node); err != nil {
				return err
			}
		}
		if i > 0 && node.Type() != ast.Array && node.Type() != ast.MultiArray {
			c.bc.Op(opcode.MemberAccess)
		}
		if i < len(n.Nodes)-1 && node.Type() != ast.Array && node.Type() != ast.MultiArray && !opcode.ObjectReturning(c.bc.LastOp()) {
			c.bc.Op(opcode.ConvToObject)
		}
	}
	return nil
}

func dynamicMemberAdd(e ast.Expr) bool {
	switch n := e.(type) {
	case *ast.Identifier:
		return true
	case *ast.Postfix:
		if len(n.Nodes) == 0 {
			return false
		}
		_, ident := n.Nodes[len(n.Nodes)-1].(*ast.Identifier)
		return ident
	default:
		return false
	}
}

func (c *Compiler) call(n *ast.FnCall) error {
	isObj := n.Object != nil
	if isObj {
		switch n.Func.Text() {
		case "lower":
			return c.objectAliasCall("lowercase", n.Object)
		case "upper":
			return c.objectAliasCall("uppercase", n.Object)
		case "substring":
			if len(n.Args) == 1 {
				n.Args = append(n.Args, &ast.IntLit{Value: -1})
			}
		}
	}
	table := calls
	if isObj {
		table = objCalls
	}
	cmd, ok := table[n.Func.Text()]
	if !ok {
		cmd = builtin{op: opcode.Call, flags: cmdUseArray | cmdReverseArgs | cmdReturn}
		if isObj {
			cmd.convert = opcode.ConvToObject
		}
	} else if cmd.flags == 0 && defaultReturnBuiltin(n.Func.Text(), isObj) {
		cmd.flags = cmdReverseArgs | cmdReturn
	}
	emitObj := func() {
		if isObj {
			c.Expr(n.Object)
		}
		if cmd.convert != opcode.None && c.bc.LastOp() != cmd.convert {
			if cmd.convert != opcode.ConvToObject || !opcode.ObjectReturning(c.bc.LastOp()) {
				c.bc.Op(cmd.convert)
			}
		}
	}
	if cmd.flags&cmdObjectFirst != 0 {
		emitObj()
	}
	if cmd.flags&cmdUseArray != 0 {
		c.bc.Op(opcode.TypeArray)
	}
	args := n.Args
	if cmd.flags&cmdReverseArgs != 0 {
		for i, j := len(args)-1, 0; i >= 0; i, j = i-1, j+1 {
			c.Expr(args[i])
			c.sigConvertReverse(args[i], cmd.sig, j)
		}
	} else {
		if len(args) == 0 && cmd.op == opcode.ObjTokenize {
			c.str(" ,", opcode.TypeString)
		} else {
			for i, a := range args {
				c.Expr(a)
				c.sigConvert(a, cmd.sig, i)
			}
		}
	}
	if cmd.flags&cmdObjectFirst == 0 {
		emitObj()
	}
	if cmd.op == opcode.Call {
		if isObj {
			if u, ok := n.Func.(*ast.Unary); ok && u.Op == "@" && dynamicMemberAdd(u.Value) {
				c.Expr(u.Value)
				c.bc.Convert(string(u.Value.Type()), string(ast.Number))
				c.bc.Op(opcode.Add)
			} else {
				c.Expr(n.Func)
			}
		} else {
			if slot, ok := c.fnCallLocals.slots[n.Func.Text()]; ok {
				c.local(opcode.GetLocal, slot)
			} else {
				c.Expr(n.Func)
			}
		}
		if isObj {
			c.bc.Op(opcode.MemberAccess)
		}
	}
	c.bc.Op(cmd.op)
	c.lastCallReturn = cmd.flags&cmdReturn != 0
	if n.Func.Text() == "join" && len(n.Args) == 1 && n.Args[0].Type() == ast.String {
		c.Joins[n.Args[0].Text()] = true
	}
	return nil
}

func (c *Compiler) objectAliasCall(name string, target ast.Expr) error {
	c.bc.Op(opcode.TypeArray)
	if err := c.Expr(target); err != nil {
		return err
	}
	c.str(name, opcode.TypeVar)
	c.bc.Op(opcode.Call)
	c.lastCallReturn = true
	return nil
}

func defaultReturnBuiltin(name string, isObj bool) bool {
	if !isObj {
		return name != "sleep" && name != "setarray"
	}
	return name != "clear"
}

func (c *Compiler) sigConvert(e ast.Expr, sig string, i int) {
	if len(sig) <= i+1 {
		return
	}
	switch sig[i+1] {
	case 'f':
		c.bc.Convert(string(e.Type()), string(ast.Number))
	case 'o':
		c.bc.Convert(string(e.Type()), string(ast.Object))
	case 's':
		c.bc.Convert(string(e.Type()), string(ast.String))
	}
}
func (c *Compiler) sigConvertReverse(e ast.Expr, sig string, i int) {
	idx := len(sig) - 2 - i
	if idx < 0 {
		return
	}
	switch sig[idx] {
	case 'f':
		c.bc.Convert(string(e.Type()), string(ast.Number))
	case 'o':
		c.bc.Convert(string(e.Type()), string(ast.Object))
	case 's':
		c.bc.Convert(string(e.Type()), string(ast.String))
	}
}
func (c *Compiler) ident(n *ast.Identifier) {
	if n.CheckReserved {
		if v, ok := reservedConst(n.Name); ok {
			c.num(v)
			return
		}
		switch n.Name {
		case "this":
			c.bc.Op(opcode.This)
			return
		case "thiso":
			c.bc.Op(opcode.ThisO)
			return
		case "player":
			c.bc.Op(opcode.Player)
			return
		case "playero":
			c.bc.Op(opcode.PlayerO)
			return
		case "level":
			c.bc.Op(opcode.Level)
			return
		case "temp":
			c.bc.Op(opcode.Temp)
			return
		case "true":
			c.bc.Op(opcode.TypeTrue)
			return
		case "false":
			c.bc.Op(opcode.TypeFalse)
			return
		case "null":
			c.bc.Op(opcode.TypeNull)
			return
		case "pi":
			c.bc.Op(opcode.Pi)
			return
		}
	}
	c.str(n.Name, opcode.TypeVar)
}
func reservedConst(name string) (int32, bool) {
	switch strings.ToUpper(name) {
	case "VK_LEFT":
		return 37, true
	case "VK_UP":
		return 38, true
	case "VK_RIGHT":
		return 39, true
	case "VK_DOWN":
		return 40, true
	case "VK_DELETE":
		return 46, true
	default:
		return 0, false
	}
}
func (c *Compiler) num(v int32)                     { c.bc.Op(opcode.TypeNumber); c.bc.DynamicNumber(v) }
func (c *Compiler) local(op opcode.Opcode, v int32) { c.bc.Op(op); c.bc.DynamicNumber(v) }
func (c *Compiler) number(v float64) {
	if math.Trunc(v) == v && v >= math.MinInt32 && v <= math.MaxInt32 {
		c.num(int32(v))
		return
	}
	s := strconv.FormatFloat(v, 'f', 9, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		s = "0"
	}
	c.bc.Op(opcode.TypeNumber)
	c.bc.DoubleNumber(s)
}
func (c *Compiler) emitCallLocals() {
	for _, name := range c.fnCallLocals.order {
		c.str(name, opcode.TypeVar)
		c.bc.Op(opcode.ResolveProperty)
		c.local(opcode.SetLocal, c.fnCallLocals.slots[name])
		c.bc.Op(opcode.IndexDec)
	}
}
func (c *Compiler) str(s string, op opcode.Opcode) {
	id := c.bc.StringID(s)
	c.bc.Op(op)
	c.bc.DynamicUnsigned(uint32(id))
}
func foldedNumber(n *ast.Binary) (float64, bool) {
	if !foldableNumberOp(n.Op) {
		return 0, false
	}
	l, ok := literalNumber(n.Left)
	if !ok {
		return 0, false
	}
	r, ok := literalNumber(n.Right)
	if !ok {
		return 0, false
	}
	switch n.Op {
	case "+":
		return l + r, true
	case "-":
		return l - r, true
	case "*":
		return l * r, true
	case "/":
		if r == 0 {
			return 0, false
		}
		return l / r, true
	case "%":
		if r == 0 {
			return 0, false
		}
		return math.Mod(l, r), true
	}
	return 0, false
}
func literalNumber(e ast.Expr) (float64, bool) {
	switch n := e.(type) {
	case *ast.IntLit:
		return float64(n.Value), true
	case *ast.ConstLit:
		v, err := strconv.ParseFloat(n.Value, 64)
		return v, err == nil
	case *ast.FloatLit:
		v, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return 0, false
		}
		return v, true
	case *ast.Unary:
		if n.Op == "-" {
			v, ok := literalNumber(n.Value)
			return -v, ok
		}
	}
	return 0, false
}
func foldableNumberOp(op string) bool {
	switch op {
	case "+", "-", "*", "/", "%":
		return true
	}
	return false
}
func repeatedBareCallLocals(s ast.Stmt) callLocals {
	counts := map[string]int{}
	var order []string
	var stmt func(ast.Stmt)
	var expr func(ast.Expr)
	add := func(name string) {
		if counts[name] == 0 {
			order = append(order, name)
		}
		counts[name]++
	}
	expr = func(e ast.Expr) {
		switch n := e.(type) {
		case nil:
		case *ast.FnCall:
			if cacheableBareCall(n) {
				add(n.Func.Text())
			}
			expr(n.Object)
			expr(n.Func)
			for _, a := range n.Args {
				expr(a)
			}
		case *ast.Postfix:
			for _, x := range n.Nodes {
				expr(x)
			}
		case *ast.ArrayIndex:
			for _, x := range n.Exprs {
				expr(x)
			}
		case *ast.Cast:
			expr(n.Value)
		case *ast.In:
			expr(n.Value)
			expr(n.Lower)
			expr(n.Higher)
		case *ast.Ternary:
			expr(n.Cond)
			expr(n.Left)
			expr(n.Right)
		case *ast.Binary:
			expr(n.Left)
			expr(n.Right)
		case *ast.Unary:
			expr(n.Value)
		case *ast.NewArray:
			for _, d := range n.Dims {
				expr(d)
			}
		case *ast.NewObject:
			for _, a := range n.Args {
				expr(a)
			}
		case *ast.List:
			for _, a := range n.Args {
				expr(a)
			}
		}
	}
	stmt = func(s ast.Stmt) {
		switch n := s.(type) {
		case nil:
		case *ast.Block:
			for _, x := range n.Stmts {
				stmt(x)
			}
		case *ast.FnDecl:
		case *ast.If:
			expr(n.Cond)
			stmt(n.Then)
			stmt(n.Else)
		case *ast.While:
			expr(n.Cond)
			stmt(n.Body)
		case *ast.DoWhile:
			expr(n.Cond)
			stmt(n.Body)
		case *ast.For:
			expr(n.Init)
			expr(n.Cond)
			expr(n.Post)
			stmt(n.Body)
		case *ast.ForEach:
			expr(n.Name)
			expr(n.Range)
			stmt(n.Body)
		case *ast.Switch:
			expr(n.Target)
			for _, cs := range n.Cases {
				for _, e := range cs.Exprs {
					expr(e)
				}
				stmt(cs.Body)
			}
		case *ast.With:
			expr(n.Target)
		case *ast.NewStmt:
			for _, a := range n.Args {
				expr(a)
			}
		case *ast.Return:
			expr(n.Value)
		case ast.Expr:
			expr(n)
		}
	}
	stmt(s)
	out := callLocals{slots: map[string]int32{}}
	for _, name := range order {
		if counts[name] > 1 {
			out.slots[name] = int32(len(out.order))
			out.order = append(out.order, name)
		}
	}
	return out
}
func cacheableBareCall(n *ast.FnCall) bool {
	if n.Object != nil {
		return false
	}
	if _, ok := n.Func.(*ast.Identifier); !ok {
		return false
	}
	_, builtin := calls[n.Func.Text()]
	return !builtin
}
func boolU32(v bool) uint32 {
	if v {
		return 1
	}
	return 0
}
func isAssignOp(op string) bool {
	switch op {
	case "=", "+=", "-=", "*=", "/=", "^=", "%=", "@=", "<<=", ">>=":
		return true
	}
	return false
}
func numericOp(op string) bool {
	switch op {
	case "+", "-", "*", "/", "%", "^", "&", "|", "xor", "<<", ">>", "<", "<=", ">", ">=":
		return true
	}
	return false
}
func opFor(op string) opcode.Opcode {
	switch op {
	case "+", "+=":
		return opcode.Add
	case "-", "-=":
		return opcode.Sub
	case "*", "*=":
		return opcode.Mul
	case "/", "/=":
		return opcode.Div
	case "%", "%=":
		return opcode.Mod
	case "^", "^=":
		return opcode.Pow
	case "&":
		return opcode.Bwa
	case "|":
		return opcode.Bwo
	case "xor":
		return opcode.Bwx
	case "<<", "<<=":
		return opcode.BwLeftShift
	case ">>", ">>=":
		return opcode.BwRightShift
	case "==":
		return opcode.Eq
	case "!=":
		return opcode.Neq
	case "<":
		return opcode.Lt
	case "<=":
		return opcode.Lte
	case ">":
		return opcode.Gt
	case ">=":
		return opcode.Gte
	case "@", "@=":
		return opcode.Join
	}
	return opcode.None
}

func hasFunctionCall(s ast.Stmt) bool {
	switch n := s.(type) {
	case nil:
		return false
	case *ast.Block:
		for _, x := range n.Stmts {
			if hasFunctionCall(x) {
				return true
			}
		}
	case *ast.If:
		return hasFunctionCall(n.Cond) || hasFunctionCall(n.Then) || hasFunctionCall(n.Else)
	case *ast.While:
		return hasFunctionCall(n.Cond) || hasFunctionCall(n.Body)
	case *ast.DoWhile:
		return hasFunctionCall(n.Cond) || hasFunctionCall(n.Body)
	case *ast.For:
		return hasFunctionCall(n.Init) || hasFunctionCall(n.Cond) || hasFunctionCall(n.Post) || hasFunctionCall(n.Body)
	case *ast.ForEach:
		return hasFunctionCall(n.Name) || hasFunctionCall(n.Range) || hasFunctionCall(n.Body)
	case *ast.With:
		return hasFunctionCall(n.Target) || hasFunctionCall(n.Body)
	case *ast.NewStmt:
		for _, a := range n.Args {
			if hasFunctionCall(a) {
				return true
			}
		}
		return hasFunctionCall(n.Body)
	case *ast.Return:
		return hasFunctionCall(n.Value)
	case *ast.Switch:
		if hasFunctionCall(n.Target) {
			return true
		}
		for _, cs := range n.Cases {
			for _, e := range cs.Exprs {
				if hasFunctionCall(e) {
					return true
				}
			}
			if hasFunctionCall(cs.Body) {
				return true
			}
		}
	case *ast.Binary:
		return hasFunctionCall(n.Left) || hasFunctionCall(n.Right)
	case *ast.Unary:
		return hasFunctionCall(n.Value)
	case *ast.Ternary:
		return hasFunctionCall(n.Cond) || hasFunctionCall(n.Left) || hasFunctionCall(n.Right)
	case *ast.In:
		return hasFunctionCall(n.Value) || hasFunctionCall(n.Lower) || hasFunctionCall(n.Higher)
	case *ast.Postfix:
		for _, x := range n.Nodes {
			if hasFunctionCall(x) {
				return true
			}
		}
	case *ast.ArrayIndex:
		for _, x := range n.Exprs {
			if hasFunctionCall(x) {
				return true
			}
		}
	case *ast.Cast:
		return hasFunctionCall(n.Value)
	case *ast.FnCall:
		return true
	case *ast.NewObject:
		for _, a := range n.Args {
			if hasFunctionCall(a) {
				return true
			}
		}
	case *ast.NewArray:
		for _, d := range n.Dims {
			if hasFunctionCall(d) {
				return true
			}
		}
	case *ast.List:
		for _, a := range n.Args {
			if hasFunctionCall(a) {
				return true
			}
		}
	case *ast.FnObject:
		return hasFunctionCall(n.Body)
	}
	return false
}
