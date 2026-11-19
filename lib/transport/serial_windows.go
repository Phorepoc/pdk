// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package transport

import (
	"reflect"

	"go.bug.st/serial"
	"golang.org/x/sys/windows"
)

// cancelIO aborts every overlapped read/write pending on the port. go.bug.st/serial's Close is only CloseHandle: with a write
// still pending in the USB serial driver (the phone stopped draining bulk-OUT) the file object stays open until the write
// completes, so the next CreateFile on COMn fails with "Serial port busy". CancelIoEx (documented for exactly this) completes the
// pending operations with ERROR_OPERATION_ABORTED; PurgeComm TXABORT/RXABORT is the portable second try.
func cancelIO(sp serial.Port) {
	_ = sp.ResetOutputBuffer()
	_ = sp.ResetInputBuffer()
	v := reflect.ValueOf(sp)
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		if f := v.Elem().FieldByName("handle"); f.IsValid() && f.CanUint() {
			if h := windows.Handle(f.Uint()); h != 0 {
				_ = windows.CancelIoEx(h, nil)
			}
		}
	}
}
