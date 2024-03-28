package lsp

import (
	"go/ast"
	"go/types"
	"strings"
)

// Builtin types
const (
	boolDoc    = "bool is the set of boolean values, true and false."
	byteDoc    = "byte is an alias for uint8 and is equivalent to uint8 in all ways. It is used, by convention, to distinguish byte values from 8-bit unsigned integer values."
	errorDoc   = "The error built-in interface type is the conventional interface for representing an error condition, with the nil value representing no error."
	intDoc     = "int is a signed integer type that is at least 32 bits in size. It is a distinct type, however, and not an alias for, say, int32."
	int8Doc    = "int8 is the set of all signed 8-bit integers. Range: -128 through 127."
	int16Doc   = "int16 is the set of all signed 16-bit integers. Range: -32768 through 32767."
	int32Doc   = "int32 is the set of all signed 32-bit integers. Range: -2147483648 through 2147483647."
	int64Doc   = "int64 is the set of all signed 64-bit integers. Range: -9223372036854775808 through 9223372036854775807."
	uintDoc    = "uint is an unsigned integer type that is at least 32 bits in size. It is a distinct type, however, and not an alias for, say, uint32."
	uint8Doc   = "uint8 is the set of all unsigned 8-bit integers. Range: 0 through 255."
	uint16Doc  = "uint16 is the set of all unsigned 16-bit integers. Range: 0 through 65535."
	uint32Doc  = "uint32 is the set of all unsigned 32-bit integers. Range: 0 through 4294967295."
	uint64Doc  = "uint64 is the set of all unsigned 64-bit integers. Range: 0 through 18446744073709551615."
	float32Doc = "float32 is the set of all IEEE-754 32-bit floating-point numbers."
	float64Doc = "float64 is the set of all IEEE-754 64-bit floating-point numbers."
	runeDoc    = "rune is an alias for int32 and is equivalent to int32 in all ways. It is used, by convention, to distinguish character values from integer values."
	stringDoc  = "string is the set of all strings of 8-bit bytes, conventionally but not necessarily representing UTF-8-encoded text. A string may be empty, but not nil. Values of string type are immutable."
	nilDoc     = "nil is a predeclared identifier representing the zero value for a pointer, channel, func, interface, map, or slice type."
)

// Builtin funcs
const (
	appendDoc  = "The append built-in function appends elements to the end of a slice. If it has sufficient capacity, the destination is resliced to accommodate the new elements. If it does not, a new underlying array will be allocated. Append returns the updated slice."
	capDoc     = "The cap built-in function returns the capacity of v, according to its type"
	clearDoc   = "The clear built-in function clears maps and slices. For maps, clear deletes all entries, resulting in an empty map. For slices, clear sets all elements up to the length of the slice to the zero value of the respective element type."
	copyDoc    = "The copy built-in function copies elements from a source slice into a destination slice. (As a special case, it also will copy bytes from a string to a slice of bytes.) The source and destination may overlap."
	deleteDoc  = "The delete built-in function deletes the element with the specified key (m[key]) from the map. If m is nil or there is no such element, delete is a no-op."
	lenDoc     = "The len built-in function returns the length of v, according to its type"
	makeDoc    = "The make built-in function allocates and initializes an object of type slice, map, or chan (only). Like new, the first argument is a type, not a value. Unlike new, make's return type is the same as the type of its argument, not a pointer to it."
	newDoc     = "The new built-in function allocates memory. The first argument is a type, not a value, and the value returned is a pointer to a newly allocated zero value of that type."
	panicDoc   = "The panic built-in function stops normal execution of the current goroutine. When a function F calls panic, normal execution of F stops immediately."
	printDoc   = "The print built-in function formats its arguments in an implementation-specific way and writes the result to standard error. Print is useful for bootstrapping and debugging."
	printlnDoc = "The println built-in function formats its arguments in an implementation-specific way and writes the result to standard error. Spaces are always added between arguments and a newline is appended."
	recoverDoc = "The recover built-in function allows a program to manage behavior of a panicking goroutine. Executing a call to recover inside a deferred function (but not any function called by it) stops the panicking sequence by restoring normal execution and retrieves the error value passed to the call of panic. If recover is called outside the deferred function it will not stop a panicking sequence."
)

func isBuiltin(i *ast.Ident, tv *types.TypeAndValue) (string, bool) {
	t := tv.Type.String()
	name := i.Name
	if strings.Contains(t, "gno.land/") {
		return "", false
	}

	if name == "nil" && t == "untyped nil" { // special case?
		return nilDoc, true
	}
	if (name == "true" || name == "false") && t == "bool" { // special case?
		return boolDoc, true
	}
	if name == t { // hover on the type itself?
		switch t {
		case "byte":
			return byteDoc, true
		case "error":
			return errorDoc, true
		case "int":
			return intDoc, true
		case "int8":
			return int8Doc, true
		case "int16":
			return int16Doc, true
		case "int32":
			return int32Doc, true
		case "int64":
			return int64Doc, true
		case "uint":
			return uintDoc, true
		case "uint8":
			return uint8Doc, true
		case "uint16":
			return uint16Doc, true
		case "uint32":
			return uint32Doc, true
		case "uint64":
			return uint64Doc, true
		case "float32":
			return float32Doc, true
		case "float64":
			return float64Doc, true
		case "rune":
			return runeDoc, true
		case "string":
			return stringDoc, true
		case "nil":
			return nilDoc, true
		}
	}

	if strings.HasPrefix(t, "func") {
		switch name {
		case "append":
			return appendDoc, true
		case "cap":
			return capDoc, true
		case "clear":
			return clearDoc, true
		case "copy":
			return copyDoc, true
		case "delete":
			return deleteDoc, true
		case "len":
			return lenDoc, true
		case "make":
			return makeDoc, true
		case "new":
			return newDoc, true
		case "panic":
			return panicDoc, true
		case "print":
			return printDoc, true
		case "println":
			return printlnDoc, true
		case "recover":
			return recoverDoc, true
		}
	}

	return "", false
}
