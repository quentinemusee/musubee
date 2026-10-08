// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

/*
#include <stdlib.h>
#include "musubee.h"

// cgo cannot express const: the exported functions take this type, which
// is identical to const uint8_t, so that the C compiler checks the generated
// declarations against the prototypes of musubee.h.
typedef const uint8_t musubee_const_byte;
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/quentinemusee/musubee/core/embedded"
)

var (
	coresLock  sync.Mutex
	cores      = make(map[C.musubee_handle]*embedded.Core)
	lastHandle C.musubee_handle
)

func lookup(handle C.musubee_handle) *embedded.Core {
	coresLock.Lock()
	defer coresLock.Unlock()
	return cores[handle]
}

// goBytes copies a C input buffer: the library keeps no reference to the
// caller's memory.
func goBytes(data *C.musubee_const_byte, length C.size_t) []byte {
	if data == nil || length == 0 {
		return nil
	}
	if uint64(length) > math.MaxInt32 {
		panic("input buffer larger than 2 GiB")
	}
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(data)), int(length))...)
}

// cBuffer copies data to memory allocated with malloc, released by
// musubee_free.
func cBuffer(data []byte) C.musubee_buffer {
	if len(data) == 0 {
		return C.musubee_buffer{}
	}
	return C.musubee_buffer{data: (*C.uint8_t)(C.CBytes(data)), len: C.size_t(len(data))}
}

// errorResponse is the response to a request that could not reach a core.
func errorResponse(message string) C.musubee_buffer {
	data, _ := json.Marshal(map[string]string{"error": message})
	return cBuffer(data)
}

// A panic must never cross into C, where it would abort the host process
// without a trace: every exported function recovers and reports it.

//export musubee_open
func musubee_open(config *C.musubee_const_byte, configLen C.size_t, errOut *C.musubee_buffer) (handle C.musubee_handle) {
	defer func() {
		if r := recover(); r != nil {
			handle = 0
			if errOut != nil {
				*errOut = cBuffer([]byte(fmt.Sprint("panic: ", r)))
			}
		}
	}()
	core, err := embedded.Open(goBytes(config, configLen))
	if err != nil {
		if errOut != nil {
			*errOut = cBuffer([]byte(err.Error()))
		}
		return 0
	}
	coresLock.Lock()
	defer coresLock.Unlock()
	lastHandle++
	cores[lastHandle] = core
	return lastHandle
}

//export musubee_call
func musubee_call(handle C.musubee_handle, request *C.musubee_const_byte, requestLen C.size_t) (resp C.musubee_buffer) {
	defer func() {
		if r := recover(); r != nil {
			resp = errorResponse(fmt.Sprint("panic: ", r))
		}
	}()
	core := lookup(handle)
	if core == nil {
		return errorResponse("invalid or closed handle")
	}
	return cBuffer(core.Call(goBytes(request, requestLen)))
}

//export musubee_next_event
func musubee_next_event(handle C.musubee_handle, timeoutMS C.int32_t) (evt C.musubee_buffer) {
	defer func() {
		if r := recover(); r != nil {
			evt = errorResponse(fmt.Sprint("panic: ", r))
		}
	}()
	core := lookup(handle)
	if core == nil {
		return cBuffer([]byte(`{"type":"closed"}`))
	}
	data, ok := core.NextEvent(time.Duration(max(timeoutMS, 0)) * time.Millisecond)
	if !ok {
		return C.musubee_buffer{}
	}
	return cBuffer(data)
}

//export musubee_close
func musubee_close(handle C.musubee_handle) {
	defer func() { _ = recover() }()
	coresLock.Lock()
	core := cores[handle]
	delete(cores, handle)
	coresLock.Unlock()
	if core != nil {
		_ = core.Close()
	}
}

//export musubee_free
func musubee_free(buffer C.musubee_buffer) {
	C.free(unsafe.Pointer(buffer.data))
}
