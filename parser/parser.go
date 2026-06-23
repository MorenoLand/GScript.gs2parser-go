package parser

import (
	"fmt"
	"strconv"

	"github.com/MorenoLand/GScript.gs2parser-go/ast"
	"github.com/MorenoLand/GScript.gs2parser-go/lexer"
)

type Parser struct {
	toks   []lexer.Token
	pos    int
	lamb   int
	consts map[string]ast.Expr
	stop   map[string]bool
}

func Parse(src string) (root *ast.Block, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", r)
			}
		}
	}()
	toks, err := lexer.Lex(src)
	if err != nil {
		return nil, err
	}
	p := &Parser{toks: toks, consts: map[string]ast.Expr{}}
	return p.program()
}

func (p *Parser) program() (*ast.Block, error) {
	b := &ast.Block{}
	for !p.atEnd() {
		s, err := p.decl()
		if err != nil {
			return nil, err
		}
		if s != nil {
			appendBlockStmt(b, s)
		}
	}
	return b, nil
}

func (p *Parser) decl() (ast.Stmt, error) {
	if p.match("const") {
		name := p.expectKind(lexer.Ident)
		p.expect("=")
		v, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		if lit, ok := v.(*ast.IntLit); ok {
			v = &ast.ConstLit{Value: strconv.Itoa(lit.Value)}
		}
		p.consts[name.Lit] = v
		p.expect(";")
		return nil, nil
	}
	if p.match("enum") {
		prefix := ""
		if p.cur().Kind == lexer.Ident {
			prefix = p.next().Lit + "::"
		}
		p.expect("{")
		idx := 0
		for !p.match("}") {
			name := p.expectKind(lexer.Ident).Lit
			if p.match("=") {
				v := p.expectKind(lexer.Int)
				idx, _ = strconv.Atoi(v.Lit)
			}
			p.consts[prefix+name] = &ast.ConstLit{Value: strconv.Itoa(idx)}
			idx++
			p.match(",")
		}
		return nil, nil
	}
	return p.stmt()
}

func (p *Parser) stmt() (ast.Stmt, error) {
	if p.match(";") {
		return nil, nil
	}
	if p.match("{") {
		b := &ast.Block{}
		for !p.match("}") {
			s, err := p.stmt()
			if err != nil {
				return nil, err
			}
			if s != nil {
				appendBlockStmt(b, s)
			}
		}
		return b, nil
	}
	pub := p.match("public")
	if p.match("function") {
		return p.fnDecl(pub)
	}
	if p.match("if") {
		return p.ifStmt()
	}
	if p.match("while") {
		p.expect("(")
		c, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		p.expect(")")
		body, err := p.stmt()
		return &ast.While{Cond: c, Body: body}, err
	}
	if p.match("do") {
		body, err := p.stmt()
		if err != nil {
			return nil, err
		}
		p.expect("while")
		p.expect("(")
		c, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		p.expect(")")
		p.expect(";")
		return &ast.DoWhile{Cond: c, Body: body}, nil
	}
	if p.match("with") {
		p.expect("(")
		t, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		p.expect(")")
		body, err := p.stmt()
		return &ast.With{Target: t, Body: body}, err
	}
	if p.match("for") {
		return p.forStmt()
	}
	if p.match("switch") {
		return p.switchStmt()
	}
	if p.match("break") {
		p.expect(";")
		return &ast.Break{}, nil
	}
	if p.match("continue") {
		p.expect(";")
		return &ast.Continue{}, nil
	}
	if p.match("return") {
		if p.match(";") {
			return &ast.Return{}, nil
		}
		v, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		p.expect(";")
		return &ast.Return{Value: v}, nil
	}
	if p.match("new") && p.cur().Kind == lexer.Ident {
		name := p.next().Lit
		if p.match("(") {
			args, err := p.exprList(")")
			if err != nil {
				return nil, err
			}
			body, err := p.stmt()
			blk, _ := body.(*ast.Block)
			return &ast.NewStmt{Name: name, Args: args, Body: blk}, err
		}
		p.pos -= 2
	}
	e, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	if p.match(",") {
		b := &ast.Block{}
		appendBlockStmt(b, e)
		for {
			next, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			appendBlockStmt(b, next)
			if !p.match(",") {
				break
			}
		}
		p.expect(";")
		return b, nil
	}
	p.expect(";")
	return e, nil
}

func (p *Parser) fnDecl(pub bool) (ast.Stmt, error) {
	first := p.expectKind(lexer.Ident).Lit
	obj, name := "", first
	if p.match(".") {
		obj = first
		name = p.expectKind(lexer.Ident).Lit
	}
	p.expect("(")
	args, err := p.exprList(")")
	if err != nil {
		return nil, err
	}
	body := &ast.Block{}
	if !p.atEnd() && !p.cur().Is(";") && !p.cur().Is("function") && !p.cur().Is("public") {
		s, err := p.stmt()
		if err != nil {
			return nil, err
		}
		if b, ok := s.(*ast.Block); ok {
			body = b
		} else if s != nil {
			appendBlockStmt(body, s)
		}
	}
	return &ast.FnDecl{Public: pub, Object: obj, Name: name, Args: args, Body: body, EmitPrejump: true}, nil
}

func (p *Parser) ifStmt() (ast.Stmt, error) {
	p.expect("(")
	c, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	p.expect(")")
	then, err := p.stmt()
	if err != nil {
		return nil, err
	}
	var els ast.Stmt
	if p.match("else") {
		els, err = p.stmt()
	} else if p.match("elseif") {
		els, err = p.ifStmt()
	}
	return &ast.If{Cond: c, Then: then, Else: els}, err
}

func appendBlockStmt(b *ast.Block, s ast.Stmt) {
	if u, ok := s.(*ast.Unary); ok && (u.Op == "++" || u.Op == "--") {
		if !postfixStmtKeepsValue(u.Value) {
			u.Prefix = true
		}
		u.Unused = true
	}
	b.Stmts = append(b.Stmts, s)
}

func postfixStmtKeepsValue(e ast.Expr) bool {
	p, ok := e.(*ast.Postfix)
	if !ok || len(p.Nodes) < 2 {
		return false
	}
	id, ok := p.Nodes[0].(*ast.Identifier)
	if !ok {
		return false
	}
	member, ok := p.Nodes[1].(*ast.Identifier)
	if !ok {
		return false
	}
	return (id.Name == "client" && member.Name == "speed2") || (id.Name == "this" && member.Name == "pcount")
}

func (p *Parser) forStmt() (ast.Stmt, error) {
	p.expect("(")
	var first ast.Expr
	if !p.cur().Is(";") {
		var err error
		first, err = p.expr(0)
		if err != nil {
			return nil, err
		}
	}
	if p.match(":") {
		r, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		p.expect(")")
		body, err := p.stmt()
		return &ast.ForEach{Name: first, Range: r, Body: body}, err
	}
	p.expect(";")
	cond, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	p.expect(";")
	post, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	if u, ok := post.(*ast.Unary); ok && (u.Op == "++" || u.Op == "--") {
		u.Prefix = true
		u.Unused = true
	}
	p.expect(")")
	body, err := p.stmt()
	return &ast.For{Init: first, Cond: cond, Post: post, Body: body}, err
}

func (p *Parser) switchStmt() (ast.Stmt, error) {
	p.expect("(")
	target, err := p.expr(0)
	if err != nil {
		return nil, err
	}
	p.expect(")")
	p.expect("{")
	sw := &ast.Switch{Target: target}
	for !p.match("}") {
		c := ast.SwitchCase{Body: &ast.Block{}}
		for {
			if p.match("case") {
				e, err := p.expr(0)
				if err != nil {
					return nil, err
				}
				c.Exprs = append(c.Exprs, e)
				p.expect(":")
			} else if p.match("default") {
				c.Exprs = append(c.Exprs, nil)
				p.expect(":")
			} else if len(c.Exprs) == 0 {
				return nil, p.err("expected case/default")
			} else {
				break
			}
			if !p.cur().Is("case") && !p.cur().Is("default") {
				break
			}
		}
		for !p.cur().Is("case") && !p.cur().Is("default") && !p.cur().Is("}") {
			s, err := p.stmt()
			if err != nil {
				return nil, err
			}
			if s != nil {
				appendBlockStmt(c.Body, s)
			}
		}
		sw.Cases = append(sw.Cases, c)
	}
	return sw, nil
}

var prec = map[string]int{
	"=": 1, "+=": 1, "-=": 1, "*=": 1, "/=": 1, "^=": 1, "%=": 1, "@=": 1, "<<=": 1, ">>=": 1,
	"?": 2, "||": 3, "&&": 4, "&": 5, "|": 5, "xor": 5, "<<": 5, ">>": 5, "@": 6,
	"<": 7, "<=": 7, ">": 7, ">=": 7, "==": 8, "!=": 8, "in": 8,
	"+": 9, "-": 9, "*": 10, "/": 10, "^": 10, "%": 10, ".": 12,
}

func (p *Parser) expr(min int) (ast.Expr, error) {
	left, err := p.prefix()
	if err != nil {
		return nil, err
	}
	for {
		op := p.cur().Lit
		if p.stop != nil && p.stop[op] {
			break
		}
		if op == "[" {
			p.next()
			args, err := p.exprList("]")
			if err != nil {
				return nil, err
			}
			left = appendPostfix(left, &ast.ArrayIndex{Exprs: args})
			continue
		}
		if op == "." {
			p.next()
			right, err := p.prefix()
			if err != nil {
				return nil, err
			}
			left = appendPostfix(left, right)
			continue
		}
		if op == "(" {
			p.next()
			args, err := p.exprList(")")
			if err != nil {
				return nil, err
			}
			if pf, ok := left.(*ast.Postfix); ok && len(pf.Nodes) > 0 {
				fn := pf.Nodes[len(pf.Nodes)-1]
				objNodes := append([]ast.Expr{}, pf.Nodes[:len(pf.Nodes)-1]...)
				var obj ast.Expr
				if len(objNodes) == 1 {
					obj = objNodes[0]
				} else if len(objNodes) > 1 {
					obj = &ast.Postfix{Nodes: objNodes}
				}
				left = &ast.FnCall{Func: fn, Object: obj, Args: args}
			} else {
				left = &ast.FnCall{Func: left, Args: args}
			}
			continue
		}
		if op == "++" || op == "--" {
			p.next()
			left = &ast.Unary{Op: op, Value: left, Prefix: false}
			continue
		}
		pv, ok := prec[op]
		if !ok && len(op) == 2 && op[0] == '@' && op != "@=" {
			pv, ok = prec["@"]
		}
		if !ok || pv < min {
			break
		}
		p.next()
		if op == "?" {
			mid, err := p.expr(0)
			if err != nil {
				return nil, err
			}
			p.expect(":")
			right, err := p.expr(pv + 1)
			if err != nil {
				return nil, err
			}
			left = &ast.Ternary{Cond: left, Left: mid, Right: right}
			continue
		}
		if op == "in" {
			var lo, hi ast.Expr
			if p.match("|") || p.match("<") {
				close := "|"
				if p.toks[p.pos-1].Lit == "<" {
					close = ">"
				}
				lo, err = p.exprUntil(",")
				if err != nil {
					return nil, err
				}
				p.expect(",")
				hi, err = p.exprUntil(close)
				if err != nil {
					return nil, err
				}
				p.expect(close)
			} else {
				lo, err = p.expr(pv + 1)
				if err != nil {
					return nil, err
				}
				if p.match(",") {
					hi, err = p.expr(pv + 1)
					if err != nil {
						return nil, err
					}
				}
			}
			left = &ast.In{Value: left, Lower: lo, Higher: hi}
			continue
		}
		nextMin := pv + 1
		if isAssign(op) {
			nextMin = pv
			left.SetAssign(true)
		}
		right, err := p.expr(nextMin)
		if err != nil {
			return nil, err
		}
		sep := byte(0)
		if len(op) == 2 && op[0] == '@' && op != "@=" {
			sep = op[1]
			op = "@"
		}
		b := &ast.Binary{Left: left, Right: right, Op: op, Sep: sep}
		if isAssign(op) {
			b.SetAssign(true)
		}
		left = b
	}
	return left, nil
}

func (p *Parser) exprUntil(stops ...string) (ast.Expr, error) {
	old := p.stop
	p.stop = map[string]bool{}
	for _, s := range stops {
		p.stop[s] = true
	}
	e, err := p.expr(0)
	p.stop = old
	return e, err
}

func (p *Parser) prefix() (ast.Expr, error) {
	t := p.next()
	switch t.Kind {
	case lexer.Int:
		v, _ := strconv.Atoi(t.Lit)
		return &ast.IntLit{Value: v}, nil
	case lexer.Float:
		return &ast.FloatLit{Value: t.Lit}, nil
	case lexer.String:
		return &ast.StringLit{Value: t.Lit}, nil
	case lexer.Ident:
		if v, ok := p.consts[t.Lit]; ok {
			return v, nil
		}
		return &ast.Identifier{Name: t.Lit, CheckReserved: true}, nil
	}
	switch t.Lit {
	case "(":
		e, err := p.expr(0)
		p.expect(")")
		return e, err
	case "{":
		args, err := p.exprList("}")
		return &ast.List{Args: args}, err
	case "-":
		e, err := p.expr(10)
		return &ast.Unary{Op: t.Lit, Value: e, Prefix: true}, err
	case "!", "~", "++", "--", "@":
		min := 11
		if t.Lit == "@" {
			min = prec["@"] + 1
		}
		e, err := p.expr(min)
		return &ast.Unary{Op: t.Lit, Value: e, Prefix: true}, err
	case "@\n", "@ ", "@\t":
		e, err := p.expr(prec["@"] + 1)
		return &ast.Unary{Op: "@", Value: e, Prefix: true}, err
	case "int":
		p.expect("(")
		e, err := p.expr(0)
		p.expect(")")
		return &ast.Cast{Kind: ast.Integer, Value: e}, err
	case "float":
		p.expect("(")
		e, err := p.expr(0)
		p.expect(")")
		return &ast.Cast{Kind: ast.Number, Value: e}, err
	case "_":
		p.expect("(")
		e, err := p.expr(0)
		p.expect(")")
		return &ast.Cast{Kind: ast.String, Value: e}, err
	case "new":
		if p.match("[") {
			var dims []ast.Expr
			for {
				e, err := p.expr(0)
				if err != nil {
					return nil, err
				}
				dims = append(dims, e)
				p.expect("]")
				if !p.match("[") {
					break
				}
			}
			return &ast.NewArray{Dims: dims}, nil
		}
		class, err := p.prefix()
		if err != nil {
			return nil, err
		}
		p.expect("(")
		args, err := p.exprList(")")
		return &ast.NewObject{Class: class, Args: args}, err
	case "function":
		p.expect("(")
		args, err := p.exprList(")")
		if err != nil {
			return nil, err
		}
		bodyStmt, err := p.stmt()
		if err != nil {
			return nil, err
		}
		body, _ := bodyStmt.(*ast.Block)
		if body == nil {
			body = &ast.Block{Stmts: []ast.Stmt{bodyStmt}}
		}
		name := fmt.Sprintf("function_%d_1", 100+p.lamb)
		p.lamb++
		return &ast.FnObject{Name: name, Args: args, Body: body}, nil
	}
	return nil, p.err("expected expression")
}

func appendPostfix(base ast.Expr, e ast.Expr) ast.Expr {
	if pf, ok := base.(*ast.Postfix); ok {
		pf.Nodes = append(pf.Nodes, e)
		return pf
	}
	if id, ok := e.(*ast.Identifier); ok {
		id.CheckReserved = false
	}
	return &ast.Postfix{Nodes: []ast.Expr{base, e}}
}
func (p *Parser) exprList(end string) ([]ast.Expr, error) {
	var args []ast.Expr
	if p.match(end) {
		return args, nil
	}
	for {
		e, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		args = append(args, e)
		if p.match(end) {
			return args, nil
		}
		p.expect(",")
		if p.match(end) {
			return args, nil
		}
	}
}
func isAssign(op string) bool {
	switch op {
	case "=", "+=", "-=", "*=", "/=", "^=", "%=", "@=", "<<=", ">>=":
		return true
	}
	return false
}
func (p *Parser) cur() lexer.Token {
	if p.pos >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.pos]
}
func (p *Parser) next() lexer.Token {
	t := p.cur()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}
func (p *Parser) match(s string) bool {
	if p.cur().Lit == s {
		p.next()
		return true
	}
	return false
}
func (p *Parser) expect(s string) lexer.Token {
	if p.cur().Lit != s {
		panic(p.err("expected " + s))
	}
	return p.next()
}
func (p *Parser) expectKind(k lexer.Kind) lexer.Token {
	if p.cur().Kind != k {
		panic(p.err("unexpected token"))
	}
	return p.next()
}
func (p *Parser) atEnd() bool { return p.cur().Kind == lexer.EOF }
func (p *Parser) err(s string) error {
	t := p.cur()
	return &Error{Message: s, Line: t.Line, Column: t.Col, Near: t.Lit}
}
