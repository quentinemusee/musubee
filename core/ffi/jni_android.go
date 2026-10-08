// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

// The Java Native Interface (JNI) entry points of the Android library: the
// methods of the Kotlin object app.musubee.core.NativeCore (apps/mobile),
// in the same library as the C functions, so the app ships one .so per ABI.
// They mirror the C API of musubee.h; Kotlin byte arrays replace the C
// buffers, and failures become IllegalStateException.

/*
#include <jni.h>
#include <stdlib.h>

static jsize musubee_jni_length(JNIEnv *env, jbyteArray array) {
	return array == NULL ? 0 : (*env)->GetArrayLength(env, array);
}

static void musubee_jni_read(JNIEnv *env, jbyteArray array, jsize len, void *dst) {
	(*env)->GetByteArrayRegion(env, array, 0, len, (jbyte *)dst);
}

static jbyteArray musubee_jni_bytes(JNIEnv *env, const void *data, jsize len) {
	jbyteArray array = (*env)->NewByteArray(env, len);
	if (array != NULL && len > 0) {
		(*env)->SetByteArrayRegion(env, array, 0, len, (const jbyte *)data);
	}
	return array;
}

static void musubee_jni_throw(JNIEnv *env, const char *message) {
	jclass cls = (*env)->FindClass(env, "java/lang/IllegalStateException");
	if (cls != NULL) {
		(*env)->ThrowNew(env, cls, message);
	}
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// jniBytes copies a Java byte array; a null array reads as empty.
func jniBytes(env *C.JNIEnv, array C.jbyteArray) []byte {
	n := C.musubee_jni_length(env, array)
	if n <= 0 {
		return nil
	}
	data := make([]byte, int(n))
	C.musubee_jni_read(env, array, n, unsafe.Pointer(&data[0]))
	return data
}

// javaBytes returns a new Java byte array holding data (null if the JVM is
// out of memory, with an OutOfMemoryError pending).
func javaBytes(env *C.JNIEnv, data []byte) C.jbyteArray {
	if len(data) == 0 {
		return C.musubee_jni_bytes(env, nil, 0)
	}
	return C.musubee_jni_bytes(env, unsafe.Pointer(&data[0]), C.jsize(len(data)))
}

func throw(env *C.JNIEnv, message string) {
	cMessage := C.CString(message)
	defer C.free(unsafe.Pointer(cMessage))
	C.musubee_jni_throw(env, cMessage)
}

// As in ffi.go, a panic must never cross into the JVM: it would abort the
// app. Each entry point recovers and throws instead.

//export Java_app_musubee_core_NativeCore_open
func Java_app_musubee_core_NativeCore_open(env *C.JNIEnv, _ C.jclass, config C.jbyteArray) (handle C.jlong) {
	defer func() {
		if r := recover(); r != nil {
			handle = 0
			throw(env, fmt.Sprint("panic: ", r))
		}
	}()
	h, err := openCore(jniBytes(env, config))
	if err != nil {
		throw(env, err.Error())
		return 0
	}
	return C.jlong(h)
}

//export Java_app_musubee_core_NativeCore_call
func Java_app_musubee_core_NativeCore_call(env *C.JNIEnv, _ C.jclass, handle C.jlong, request C.jbyteArray) (resp C.jbyteArray) {
	defer func() {
		if r := recover(); r != nil {
			resp = 0
			throw(env, fmt.Sprint("panic: ", r))
		}
	}()
	core := lookup(uint64(handle))
	if core == nil {
		throw(env, closedHandleError)
		return 0
	}
	return javaBytes(env, core.Call(jniBytes(env, request)))
}

//export Java_app_musubee_core_NativeCore_nextEvent
func Java_app_musubee_core_NativeCore_nextEvent(env *C.JNIEnv, _ C.jclass, handle C.jlong, timeoutMS C.jint) (evt C.jbyteArray) {
	defer func() {
		if r := recover(); r != nil {
			evt = 0
			throw(env, fmt.Sprint("panic: ", r))
		}
	}()
	data, ok := nextEvent(uint64(handle), int32(timeoutMS))
	if !ok {
		return 0
	}
	return javaBytes(env, data)
}

//export Java_app_musubee_core_NativeCore_close
func Java_app_musubee_core_NativeCore_close(env *C.JNIEnv, _ C.jclass, handle C.jlong) {
	defer func() {
		if r := recover(); r != nil {
			throw(env, fmt.Sprint("panic: ", r))
		}
	}()
	closeCore(uint64(handle))
}
