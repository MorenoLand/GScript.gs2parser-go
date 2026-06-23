package opcode

type Opcode byte

const (
	None Opcode = iota
	SetIndex
	SetIndexTrue
	Or
	If
	And
	Call
	Ret
	Sleep
	CmdCall
	Jmp
	WaitFor
)

const (
	TypeNumber Opcode = 20 + iota
	TypeString
	TypeVar
	TypeArray
	TypeTrue
	TypeFalse
	TypeNull
	Pi
)

const (
	CopyLastOps Opcode = 30 + iota
	SwapLastOps
	IndexDec
	ConvToFloat
	ConvToString
	MemberAccess
	ConvToObject
	ArrayEnd
	ArrayNew
	SetArray
	InlineNew
	MakeVar
	NewObject
	ObjFromStr
	InlineConditional
	Unknown45
	Unknown46
	Unknown47
)

const (
	SetLocal        = Unknown45
	GetLocal        = Unknown46
	ResolveProperty = Unknown47
)

const (
	Assign Opcode = 50 + iota
	FuncParamsEnd
	Inc
	Dec
	Unknown54
)

const (
	Add Opcode = 60 + iota
	Sub
	Mul
	Div
	Mod
	Pow
	Unknown66
	Unknown67
	Not
	UnarySub
	Eq
	Neq
	Lt
	Gt
	Lte
	Gte
	Bwo
	Bwa
	Bwx
	Bwi
	InRange
	InObj
	ObjIndex
	ObjType
	Format
	Int
	Abs
	Random
	Sin
	Cos
	Arctan
	Exp
	Log
	Min
	Max
	GetAngle
	GetDir
	VecX
	VecY
	ObjIndices
	ObjLink
	BwLeftShift
	BwRightShift
	Char
	ObjCompare
)

const (
	ObjTrim Opcode = 110 + iota
	ObjLength
	ObjPos
	Join
	ObjCharAt
	ObjSubstr
	ObjStarts
	ObjEnds
	ObjTokenize
	Translate
	ObjPositions
)

const (
	ObjSize Opcode = 130 + iota
	Array
	ArrayAssign
	ArrayMultidim
	ArrayMultidimAssign
	ObjSubarray
	ObjAddString
	ObjDeleteString
	ObjRemoveString
	ObjReplaceString
	ObjInsertString
	ObjClear
	ArrayNewMultidim
)

const (
	With Opcode = 150 + iota
	WithEnd
)

const (
	ForEach Opcode = 163
	This    Opcode = 180
	ThisO   Opcode = 181
	Player  Opcode = 182
	PlayerO Opcode = 183
	Level   Opcode = 184
	Temp    Opcode = 189
	Params  Opcode = 190
)

func BooleanReturning(op Opcode) bool {
	switch op {
	case Not, Eq, Neq, Lt, Gt, Lte, Gte, InRange, InObj:
		return true
	default:
		return false
	}
}

func ObjectReturning(op Opcode) bool {
	switch op {
	case This, ThisO, Player, PlayerO, Level, Temp:
		return true
	default:
		return false
	}
}
