package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"duanjuapp/native/core"
	"unsafe"
)

//export DuanjuRequest
func DuanjuRequest(input *C.char) *C.char {
	if input == nil {
		return C.CString(`{"ok":false,"error":"请求为空"}`)
	}
	return C.CString(core.NativeRequest(C.GoString(input)))
}

//export DuanjuFree
func DuanjuFree(value *C.char) { C.free(unsafe.Pointer(value)) }

func main() {}
