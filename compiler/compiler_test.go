package compiler

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/MorenoLand/GScript.gs2parser-go/bytecode"
	"github.com/MorenoLand/GScript.gs2parser-go/opcode"
	"github.com/MorenoLand/GScript.gs2parser-go/parser"
)

func TestCompileBasic(t *testing.T) {
	root, err := parser.Parse(`function onCreated() { temp.a = 1 + 2; }`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Compile(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) == 0 {
		t.Fatal("empty bytecode")
	}
}

func TestCompileFoldsLiteralNumericBinaryExpressions(t *testing.T) {
	ops := compileOps(t, `function onCreated() { temp.clientwidth = 700 - 8; }`)
	if bytes.Contains(ops, []byte{byte(opcode.Sub)}) {
		t.Fatal("literal numeric subtraction should be folded before bytecode emission")
	}
	if !bytes.Contains(ops, []byte{byte(opcode.TypeNumber), 0xF4, 0x02, 0xB4}) {
		t.Fatal("expected folded numeric literal 692")
	}
}

func TestCompileCachesRepeatedBareFunctionCallsInLocals(t *testing.T) {
	src := `function onCreated() {
  if (isObject("A")) A.destroy();
  if (isObject("B")) B.destroy();
}`
	code := compileCode(t, src)
	ops := segment(t, code, bytecode.SegmentBytecode)
	id := stringID(t, segment(t, code, bytecode.SegmentStringTable), "isObject")
	ref := []byte{byte(opcode.TypeVar), 0xF0, byte(id), byte(opcode.ResolveProperty), byte(opcode.SetLocal), 0xF3, 0x00, byte(opcode.IndexDec)}
	if !bytes.Contains(ops, ref) {
		t.Fatal("expected repeated bare function call to be cached in local slot")
	}
	if bytes.Count(ops, []byte{byte(opcode.TypeVar), 0xF0, byte(id)}) != 1 {
		t.Fatal("expected bare function name to be emitted once")
	}
	if bytes.Count(ops, []byte{byte(opcode.GetLocal), 0xF3, 0x00, byte(opcode.Call)}) != 2 {
		t.Fatal("expected repeated calls to use cached local slot")
	}
}

func TestCompilePopsSingleLineIfCallReturn(t *testing.T) {
	code := compileCode(t, `function onCreated() {
  if (isObject("A")) A.destroy();
}`)
	ops := segment(t, code, bytecode.SegmentBytecode)
	id := stringID(t, segment(t, code, bytecode.SegmentStringTable), "destroy")
	if !bytes.Contains(ops, []byte{byte(opcode.TypeVar), 0xF0, byte(id), byte(opcode.MemberAccess), byte(opcode.Call), byte(opcode.IndexDec)}) {
		t.Fatal("expected single-line if call result to be popped")
	}
}

func TestCompileConvertsTernaryBeforeNumericBinaryOp(t *testing.T) {
	ops := compileOps(t, `function onCreated() {
  temp.y = (temp.flag ? 0 : 24) + 24;
}`)
	if !bytes.Contains(ops, []byte{byte(opcode.TypeNumber), 0xF3, 0x18, byte(opcode.ConvToFloat), byte(opcode.TypeNumber), 0xF3, 0x18, byte(opcode.Add)}) {
		t.Fatal("expected ternary numeric value to be converted before addition")
	}
}

func TestCompileForeachCleansIteratorStack(t *testing.T) {
	ops := compileOps(t, `function onCreated() {
  for (temp.d: {"up", "left"}) temp.j++;
}`)
	if !bytes.Contains(ops, []byte{byte(opcode.SetIndex), 0xF4}) || !bytes.Contains(ops, []byte{byte(opcode.IndexDec), byte(opcode.IndexDec), byte(opcode.IndexDec)}) {
		t.Fatal("expected foreach to pop iterator stack values after loop")
	}
}

func TestCompileMinMaxPreservesArgumentOrder(t *testing.T) {
	code := compileCode(t, `function onCreated() {
  temp.cursor = max(temp.cursor - 1, 0);
}`)
	ops := segment(t, code, bytecode.SegmentBytecode)
	cursor := stringID(t, segment(t, code, bytecode.SegmentStringTable), "cursor")
	if !bytes.Contains(ops, []byte{byte(opcode.Temp), byte(opcode.TypeVar), 0xF0, byte(cursor), byte(opcode.MemberAccess), byte(opcode.ConvToFloat), byte(opcode.TypeNumber), 0xF3, 0x01, byte(opcode.Sub), byte(opcode.TypeNumber), 0xF3, 0x00, byte(opcode.Max)}) {
		t.Fatal("expected max arguments in source order")
	}
}

func compileOps(t *testing.T, src string) []byte {
	t.Helper()
	return segment(t, compileCode(t, src), bytecode.SegmentBytecode)
}

func compileCode(t *testing.T, src string) []byte {
	t.Helper()
	root, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Compile(root)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func segment(t *testing.T, code []byte, want uint32) []byte {
	t.Helper()
	for len(code) >= 8 {
		id := binary.BigEndian.Uint32(code[:4])
		n := int(binary.BigEndian.Uint32(code[4:8]))
		code = code[8:]
		if n > len(code) {
			t.Fatalf("bad segment length %d", n)
		}
		if id == want {
			return code[:n]
		}
		code = code[n:]
	}
	t.Fatalf("segment %d not found", want)
	return nil
}

func stringID(t *testing.T, data []byte, want string) int {
	t.Helper()
	id := 0
	for len(data) > 0 {
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			break
		}
		if string(data[:end]) == want {
			return id
		}
		id++
		data = data[end+1:]
	}
	t.Fatalf("string %q not found", want)
	return 0
}
