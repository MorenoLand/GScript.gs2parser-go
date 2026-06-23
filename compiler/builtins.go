package compiler

import "github.com/MorenoLand/GScript.gs2parser-go/opcode"

type flags uint8

const (
	cmdReturn flags = 1 << iota
	cmdObjectFirst
	cmdUseArray
	cmdReverseArgs
)

type builtin struct {
	op      opcode.Opcode
	flags   flags
	sig     string
	convert opcode.Opcode
}

var calls = map[string]builtin{
	"sleep": {op: opcode.Sleep, sig: "-f"}, "sin": {op: opcode.Sin, sig: "ff"}, "char": {op: opcode.Char, sig: "ff"}, "cos": {op: opcode.Cos, sig: "ff"}, "arctan": {op: opcode.Arctan, sig: "ff"},
	"vecx": {op: opcode.VecX, sig: "ff"}, "vecy": {op: opcode.VecY, sig: "ff"}, "abs": {op: opcode.Abs, sig: "ff"}, "exp": {op: opcode.Exp, sig: "ff"}, "log": {op: opcode.Log, sig: "fff"},
	"min": {op: opcode.Min, flags: cmdReturn, sig: "fff"}, "max": {op: opcode.Max, flags: cmdReturn, sig: "fff"}, "pow": {op: opcode.Pow, flags: cmdReturn, sig: "fff"}, "random": {op: opcode.Random, sig: "fff"},
	"arraylen": {op: opcode.ObjSize, sig: "fo"}, "sarraylen": {op: opcode.ObjSize, sig: "fo"}, "setarray": {op: opcode.SetArray, sig: "-of"},
	"getangle": {op: opcode.GetAngle, flags: cmdReturn, sig: "fff"}, "getdir": {op: opcode.GetDir, flags: cmdReturn, sig: "fff"}, "waitfor": {op: opcode.WaitFor, flags: cmdReturn, sig: "xssf"},
	"format": {op: opcode.Format, flags: cmdUseArray | cmdReverseArgs | cmdReturn, sig: "xs"}, "makevar": {op: opcode.MakeVar, sig: "s", convert: opcode.ConvToString},
}

var objCalls = map[string]builtin{
	"index": {op: opcode.ObjIndex, flags: cmdObjectFirst | cmdReturn, sig: "fx", convert: opcode.ConvToObject}, "type": {op: opcode.ObjType, convert: opcode.ConvToObject},
	"indices": {op: opcode.ObjIndices}, "link": {op: opcode.ObjLink}, "trim": {op: opcode.ObjTrim, convert: opcode.ConvToString}, "length": {op: opcode.ObjLength, convert: opcode.ConvToString},
	"pos": {op: opcode.ObjPos, flags: cmdObjectFirst | cmdReturn, sig: "fs", convert: opcode.ConvToString}, "charat": {op: opcode.ObjCharAt, flags: cmdObjectFirst | cmdReturn, sig: "sf", convert: opcode.ConvToString},
	"substring": {op: opcode.ObjSubstr, flags: cmdObjectFirst | cmdReturn, sig: "sff", convert: opcode.ConvToString}, "starts": {op: opcode.ObjStarts, flags: cmdObjectFirst | cmdReturn, convert: opcode.ConvToString},
	"ends": {op: opcode.ObjEnds, flags: cmdObjectFirst | cmdReturn, convert: opcode.ConvToString}, "tokenize": {op: opcode.ObjTokenize, flags: cmdObjectFirst | cmdReturn, convert: opcode.ConvToString},
	"positions": {op: opcode.ObjPositions, flags: cmdObjectFirst | cmdReturn, sig: "os", convert: opcode.ConvToString}, "size": {op: opcode.ObjSize, convert: opcode.ConvToObject},
	"subarray": {op: opcode.ObjSubarray}, "clear": {op: opcode.ObjClear, convert: opcode.ConvToObject}, "add": {op: opcode.ObjAddString, flags: cmdObjectFirst, convert: opcode.ConvToObject},
	"delete": {op: opcode.ObjDeleteString, flags: cmdObjectFirst, convert: opcode.ConvToObject}, "insert": {op: opcode.ObjInsertString, flags: cmdObjectFirst | cmdReverseArgs, convert: opcode.ConvToObject},
	"remove": {op: opcode.ObjRemoveString, flags: cmdObjectFirst | cmdReverseArgs, convert: opcode.ConvToObject}, "replace": {op: opcode.ObjReplaceString, flags: cmdObjectFirst | cmdReverseArgs, convert: opcode.ConvToObject},
}
