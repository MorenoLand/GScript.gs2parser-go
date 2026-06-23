package bytecode

import (
	"bytes"
	"encoding/binary"
	"math"

	"github.com/MorenoLand/GScript.gs2parser-go/opcode"
)

const (
	SegmentGS1Flags      = 1
	SegmentFunctionTable = 2
	SegmentStringTable   = 3
	SegmentBytecode      = 4
)

type FunctionEntry struct {
	Name    string
	OpIndex uint32
	JumpLoc int
}

type Builder struct {
	buf        []byte
	opIndex    uint32
	last       opcode.Opcode
	strings    []string
	stringID   map[string]int32
	functions  []FunctionEntry
	functionID map[string]bool
}

func New() *Builder                      { return &Builder{stringID: map[string]int32{}, functionID: map[string]bool{}} }
func (b *Builder) LastOp() opcode.Opcode { return b.last }
func (b *Builder) OpIndex() uint32       { return b.opIndex }
func (b *Builder) Pos() int              { return len(b.buf) }
func (b *Builder) AddFunction(name string, op uint32, jumpLoc int) {
	if b.functionID[name] {
		return
	}
	b.functionID[name] = true
	b.functions = append(b.functions, FunctionEntry{name, op, jumpLoc})
}
func (b *Builder) StringID(s string) int32 {
	if id, ok := b.stringID[s]; ok {
		return id
	}
	id := int32(len(b.strings))
	b.strings = append(b.strings, s)
	b.stringID[s] = id
	return id
}
func (b *Builder) Op(op opcode.Opcode)     { b.buf = append(b.buf, byte(op)); b.last = op; b.opIndex++ }
func (b *Builder) Byte(v byte, pos ...int) { b.patchOrAppend([]byte{v}, pos...) }
func (b *Builder) Short(v int16, pos ...int) {
	var tmp [2]byte
	binary.BigEndian.PutUint16(tmp[:], uint16(v))
	b.patchOrAppend(tmp[:], pos...)
}
func (b *Builder) Int(v int32, pos ...int) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(v))
	b.patchOrAppend(tmp[:], pos...)
}
func (b *Builder) String(v string) { b.buf = append(b.buf, v...); b.buf = append(b.buf, 0) }
func (b *Builder) patchOrAppend(data []byte, pos ...int) {
	if len(pos) > 0 {
		copy(b.buf[pos[0]:pos[0]+len(data)], data)
		return
	}
	b.buf = append(b.buf, data...)
}
func (b *Builder) DynamicNumber(v int32) {
	offset := byte(0)
	switch b.last {
	case opcode.SetIndex, opcode.SetIndexTrue, opcode.TypeNumber, opcode.SetLocal, opcode.GetLocal:
		offset = 3
	case opcode.TypeVar, opcode.TypeString:
	default:
		return
	}
	if v >= math.MinInt8 && v <= math.MaxInt8 {
		b.Byte(0xF0 + offset)
		b.Byte(byte(int8(v)))
		return
	}
	if v >= math.MinInt16 && v <= math.MaxInt16 {
		b.Byte(0xF1 + offset)
		b.Short(int16(v))
		return
	}
	b.Byte(0xF2 + offset)
	b.Int(v)
}
func (b *Builder) DynamicUnsigned(v uint32) {
	offset := byte(0)
	switch b.last {
	case opcode.TypeVar, opcode.TypeString:
	case opcode.SetIndex, opcode.SetIndexTrue, opcode.TypeNumber:
		offset = 3
	default:
		return
	}
	if v <= math.MaxUint8 {
		b.Byte(0xF0 + offset)
		b.Byte(byte(v))
		return
	}
	if v <= math.MaxUint16 {
		b.Byte(0xF1 + offset)
		b.Short(int16(v))
		return
	}
	b.Byte(0xF2 + offset)
	b.Int(int32(v))
}
func (b *Builder) DoubleNumber(s string) { b.Byte(0xF6); b.String(s) }
func (b *Builder) Convert(src, dst string) bool {
	if src == dst {
		return false
	}
	before := b.opIndex
	switch dst {
	case "number":
		if src != "integer" {
			b.Op(opcode.ConvToFloat)
		}
	case "string":
		b.Op(opcode.ConvToString)
	case "object":
		b.Op(opcode.ConvToObject)
	}
	return b.opIndex != before
}
func (b *Builder) Bytes() []byte {
	b.Op(opcode.Ret)
	var out bytes.Buffer
	writeSeg := func(id uint32, data []byte) {
		binary.Write(&out, binary.BigEndian, id)
		binary.Write(&out, binary.BigEndian, uint32(len(data)))
		out.Write(data)
	}
	var flags bytes.Buffer
	binary.Write(&flags, binary.BigEndian, uint32(0))
	writeSeg(SegmentGS1Flags, flags.Bytes())
	var ft bytes.Buffer
	visited := map[string]bool{}
	for _, s := range b.strings {
		if b.functionID[s] && !visited[s] {
			writeFunc(&ft, b.functions, s)
			visited[s] = true
		}
	}
	for _, f := range b.functions {
		if b.functionID[f.Name] && !visited[f.Name] {
			writeFunc(&ft, b.functions, f.Name)
			visited[f.Name] = true
		}
		if f.JumpLoc != 0 {
			if f.JumpLoc >= 5 && b.buf[f.JumpLoc-5] == 0xF5 {
				b.Int(int32(b.opIndex), f.JumpLoc-4)
			} else {
				b.Short(int16(b.opIndex), f.JumpLoc-2)
			}
		}
	}
	writeSeg(SegmentFunctionTable, ft.Bytes())
	var st bytes.Buffer
	for _, s := range b.strings {
		st.WriteString(s)
		st.WriteByte(0)
	}
	writeSeg(SegmentStringTable, st.Bytes())
	writeSeg(SegmentBytecode, b.buf)
	out.WriteByte('\n')
	return out.Bytes()
}
func writeFunc(buf *bytes.Buffer, funcs []FunctionEntry, name string) {
	for _, f := range funcs {
		if f.Name == name {
			binary.Write(buf, binary.BigEndian, f.OpIndex)
			buf.WriteString(f.Name)
			buf.WriteByte(0)
			return
		}
	}
}
