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
	"fmt"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/embedded"
)

// The cores opened through this library, by handle. The C exports below
// and the JNI exports of jni_android.go share them.
var (
	coresLock  sync.Mutex
	cores      = make(map[uint64]*embedded.Core)
	lastHandle uint64
)

func lookup(handle uint64) *embedded.Core {
	coresLock.Lock()
	defer coresLock.Unlock()
	return cores[handle]
}

// openCore opens a core and returns its handle, never 0.
func openCore(config []byte) (uint64, error) {
	core, err := embedded.Open(config)
	if err != nil {
		return 0, err
	}
	coresLock.Lock()
	defer coresLock.Unlock()
	lastHandle++
	cores[lastHandle] = core
	return lastHandle, nil
}

// closedHandleError is the message of the closed error that answers a
// request on a handle that is not (or no longer) open.
const closedHandleError = "invalid or closed handle"

// call runs a request on a core, or answers that the handle is closed.
func call(handle uint64, request []byte) []byte {
	core := lookup(handle)
	if core == nil {
		return embedded.ErrorResponse(request, api.ErrorCodeClosed, closedHandleError)
	}
	return core.Call(request)
}

// nextEvent waits for the next event of a core; see embedded.Core.NextEvent.
// A closed handle reads core.closed, like a closed core.
func nextEvent(handle uint64, timeoutMS int32) ([]byte, bool) {
	core := lookup(handle)
	if core == nil {
		return embedded.ClosedEvent(), true
	}
	return core.NextEvent(time.Duration(max(timeoutMS, 0)) * time.Millisecond)
}

// closeCore closes a core and forgets its handle.
func closeCore(handle uint64) {
	coresLock.Lock()
	core := cores[handle]
	delete(cores, handle)
	coresLock.Unlock()
	if core != nil {
		_ = core.Close()
	}
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
	h, err := openCore(goBytes(config, configLen))
	if err != nil {
		if errOut != nil {
			*errOut = cBuffer([]byte(err.Error()))
		}
		return 0
	}
	return C.musubee_handle(h)
}

//export musubee_call
func musubee_call(handle C.musubee_handle, request *C.musubee_const_byte, requestLen C.size_t) (resp C.musubee_buffer) {
	var data []byte
	defer func() {
		if r := recover(); r != nil {
			resp = cBuffer(embedded.ErrorResponse(data, api.ErrorCodeInternal, fmt.Sprint("panic: ", r)))
		}
	}()
	data = goBytes(request, requestLen)
	return cBuffer(call(uint64(handle), data))
}

//export musubee_next_event
func musubee_next_event(handle C.musubee_handle, timeoutMS C.int32_t) (evt C.musubee_buffer) {
	defer func() {
		if r := recover(); r != nil {
			// There is no error event: the host sees the end of the stream.
			evt = cBuffer(embedded.ClosedEvent())
		}
	}()
	data, ok := nextEvent(uint64(handle), int32(timeoutMS))
	if !ok {
		return C.musubee_buffer{}
	}
	return cBuffer(data)
}

//export musubee_close
func musubee_close(handle C.musubee_handle) {
	defer func() { _ = recover() }()
	closeCore(uint64(handle))
}

//export musubee_free
func musubee_free(buffer C.musubee_buffer) {
	C.free(unsafe.Pointer(buffer.data))
}
