package compiler

import (
	"fmt"

	"gs2parser/ast"
	"gs2parser/bytecode"
	"gs2parser/opcode"
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
	Joins                          map[string]bool
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
	c := &Compiler{bc: bytecode.New(), locs: map[uint32][]int{}, addrs: map[uint32]uint32{}, inline: true, Joins: map[string]bool{}}
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
		c.bc.Byte(0xF4)
		c.bc.Short(0)
		c.at(c.brk, c.bc.Pos()-2)
	case *ast.Continue:
		if c.cont == 0 {
			return nil
		}
		c.bc.Op(opcode.SetIndex)
		c.bc.Byte(0xF4)
		c.bc.Short(0)
		c.at(c.cont, c.bc.Pos()-2)
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
		if c.lastCallReturn && topCall && c.directBlock {
			c.bc.Op(opcode.IndexDec)
			c.lastCallReturn = false
		}
	default:
		return fmt.Errorf("unhandled statement %T", s)
	}
	return nil
}

func (c *Compiler) fn(n *ast.FnDecl) error {
	jmpLoc := 0
	if n.EmitPrejump {
		c.bc.Op(opcode.SetIndex)
		c.bc.Byte(0xF4)
		c.bc.Short(0)
		jmpLoc = c.bc.Pos()
	}
	name := n.Name
	if n.Object != "" {
		name = n.Object + "." + name
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
	if err := c.Stmt(n.Body); err != nil {
		return err
	}
	if c.bc.LastOp() != opcode.Ret {
		c.num(0)
		c.bc.Op(opcode.Ret)
	}
	return nil
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
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	c.at(f, c.bc.Pos()-2)
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
		c.bc.Byte(0xF4)
		c.bc.Short(0)
		loc := c.bc.Pos() - 2
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
	c.brk, c.cont = c.label(), c.label()
	c.set(c.cont, c.bc.OpIndex())
	c.inline = false
	if err := c.Expr(n.Cond); err != nil {
		return err
	}
	c.inline = true
	c.bc.Convert(string(n.Cond.Type()), string(ast.Number))
	c.bc.Op(opcode.If)
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	c.at(c.brk, c.bc.Pos()-2)
	c.bc.Op(opcode.CmdCall)
	if err := c.Stmt(n.Body); err != nil {
		return err
	}
	c.bc.Op(opcode.SetIndex)
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	c.at(c.cont, c.bc.Pos()-2)
	c.set(c.brk, c.bc.OpIndex())
	c.success, c.fail, c.brk, c.cont = os, of, ob, oc
	return nil
}

func (c *Compiler) forStmt(n *ast.For) error {
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
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	c.at(c.brk, c.bc.Pos()-2)
	c.bc.Op(opcode.CmdCall)
	if err := c.Stmt(n.Body); err != nil {
		return err
	}
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
	c.Expr(n.Name)
	c.Expr(n.Range)
	c.bc.Op(opcode.ConvToObject)
	c.num(0)
	ob, oc := c.brk, c.cont
	c.brk, c.cont = c.label(), c.label()
	start := c.bc.OpIndex()
	c.bc.Op(opcode.ForEach)
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	c.at(c.brk, c.bc.Pos()-2)
	c.bc.Op(opcode.CmdCall)
	if err := c.Stmt(n.Body); err != nil {
		return err
	}
	c.set(c.cont, c.bc.OpIndex())
	c.bc.Op(opcode.Inc)
	c.bc.Op(opcode.SetIndex)
	c.bc.DynamicNumber(int32(start))
	c.set(c.brk, c.bc.OpIndex())
	c.brk, c.cont = ob, oc
	c.bc.Op(opcode.IndexDec)
	return nil
}

func (c *Compiler) switchStmt(n *ast.Switch) error {
	ob, oc, os, of := c.brk, c.cont, c.success, c.fail
	c.brk = c.label()
	var caseLabels []uint32
	c.bc.Op(opcode.SetIndex)
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	caseTestLoc := c.bc.Pos() - 2
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
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	loc := c.bc.Pos() - 2
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
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	loc := c.bc.Pos() - 2
	if n.Body != nil {
		if err := c.Stmt(n.Body); err != nil {
			return err
		}
	}
	c.bc.Op(opcode.WithEnd)
	c.bc.Short(int16(c.bc.OpIndex()), loc)
	return nil
}

func (c *Compiler) Expr(e ast.Expr) error {
	switch n := e.(type) {
	case *ast.IntLit:
		c.num(int32(n.Value))
	case *ast.FloatLit:
		c.bc.Op(opcode.TypeNumber)
		c.bc.DoubleNumber(n.Value)
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
		c.num(int32(n.Dims[0]))
		c.bc.Op(opcode.ArrayNew)
		for _, d := range n.Dims[1:] {
			c.num(int32(d))
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
		c.bc.Byte(0xF4)
		c.bc.Short(0)
		loc := c.bc.Pos()
		c.fn(&ast.FnDecl{Public: true, Name: n.Name, Args: n.Args, Body: n.Body})
		c.bc.Short(int16(c.bc.OpIndex()), loc-2)
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
	c.Expr(n.Left)
	if numericOp(n.Op) {
		c.bc.Convert(string(n.Left.Type()), string(ast.Number))
	}
	c.Expr(n.Right)
	if numericOp(n.Op) {
		c.bc.Convert(string(n.Right.Type()), string(ast.Number))
	}
	c.bc.Op(opFor(n.Op))
	return nil
}

func (c *Compiler) unary(n *ast.Unary) error {
	if n.Op == "-" {
		if lit, ok := n.Value.(*ast.IntLit); ok {
			c.num(int32(-lit.Value))
			return nil
		}
		if lit, ok := n.Value.(*ast.FloatLit); ok {
			c.bc.Op(opcode.TypeNumber)
			c.bc.DoubleNumber("-" + lit.Value)
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
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	c.at(fail, c.bc.Pos()-2)
	c.Expr(n.Left)
	c.set(fail, c.bc.OpIndex()+1)
	c.bc.Op(opcode.SetIndex)
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	c.at(succ, c.bc.Pos()-2)
	c.Expr(n.Right)
	c.set(succ, c.bc.OpIndex())
	return nil
}

func (c *Compiler) logical(n *ast.Binary) error {
	first := !c.inside
	inlineLogical := c.inlineLogical
	os, of := c.success, c.fail
	var label uint32
	if first {
		c.inside = true
		if c.inline {
			c.inlineLogical = true
			label = c.label()
			c.success, c.fail = label, label
		}
	}
	parent := c.logicalParent
	if n.Op == "||" {
		nextFail := c.label()
		c.fail = nextFail
		c.logicalParent = n.Op
		c.Expr(n.Left)
		c.logicalParent = parent
		c.bc.Convert(string(n.Left.Type()), string(ast.Number))
		c.bc.Op(opcode.Or)
		c.bc.Byte(0xF4)
		c.bc.Short(0)
		c.at(c.success, c.bc.Pos()-2)
		c.set(nextFail, c.bc.OpIndex())
		c.success, c.fail = os, of
		c.logicalParent = n.Op
		c.Expr(n.Right)
		c.logicalParent = parent
		c.bc.Convert(string(n.Right.Type()), string(ast.Number))
		if first && c.inline {
			if label != 0 {
				c.set(label, c.bc.OpIndex())
			}
			c.bc.Op(opcode.InlineConditional)
		}
		if first {
			c.inside = false
			c.inlineLogical = inlineLogical
			c.success, c.fail = os, of
		}
		return nil
	}
	c.logicalParent = n.Op
	c.Expr(n.Left)
	c.logicalParent = parent
	c.bc.Convert(string(n.Left.Type()), string(ast.Number))
	if n.Op == "&&" && !c.inline {
		c.bc.Op(opcode.If)
	} else if n.Op == "&&" {
		c.bc.Op(opcode.And)
	} else {
		c.bc.Op(opcode.Or)
	}
	c.bc.Byte(0xF4)
	c.bc.Short(0)
	loc := c.bc.Pos() - 2
	c.logicalParent = n.Op
	c.Expr(n.Right)
	c.logicalParent = parent
	c.bc.Convert(string(n.Right.Type()), string(ast.Number))
	target := c.bc.OpIndex()
	if !first && n.Op == "&&" && parent == "||" {
		target++
	}
	if n.Op == "&&" && (!c.inline || (c.inlineLogical && parent != "||")) {
		c.at(c.fail, loc)
	} else if n.Op == "||" && (!c.inline || c.inlineLogical) {
		c.at(c.success, loc)
	} else {
		c.bc.Short(int16(target), loc)
	}
	if first && c.inline {
		if label != 0 {
			c.set(label, c.bc.OpIndex())
		}
		c.bc.Op(opcode.InlineConditional)
	}
	if first {
		c.inside = false
		c.inlineLogical = inlineLogical
		c.success, c.fail = os, of
	}
	return nil
}

func (c *Compiler) postfix(n *ast.Postfix) error {
	for i, node := range n.Nodes {
		if err := c.Expr(node); err != nil {
			return err
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

func (c *Compiler) call(n *ast.FnCall) error {
	isObj := n.Object != nil
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
		for i := len(args) - 1; i >= 0; i-- {
			c.Expr(args[i])
			c.sigConvert(args[i], cmd.sig, len(args)-1-i)
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
		c.Expr(n.Func)
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
func (c *Compiler) ident(n *ast.Identifier) {
	if n.CheckReserved {
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
		case "params":
			c.bc.Op(opcode.Params)
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
func (c *Compiler) num(v int32) { c.bc.Op(opcode.TypeNumber); c.bc.DynamicNumber(v) }
func (c *Compiler) str(s string, op opcode.Opcode) {
	id := c.bc.StringID(s)
	c.bc.Op(op)
	c.bc.DynamicUnsigned(uint32(id))
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
