// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package phlink

import "testing"

func TestParseDump(t *testing.T) {
	tab := &symTab{imgs: []symImage{{Addr: 0x80001000, Size: 0x1000, Name: "euser.dll",
		Fn: []uint64{0x80001000, 0x80001100}, FnName: []string{"Exec::WaitForAnyRequest()", "CActiveScheduler::Start()"}}}}
	lines := []string{
		"[EKERN][mon] free RAM KB=0x000c5000",
		"[dump] ===== thread dump =====",
		"[dump] ekern.exe::Null ms=0x00000002",
		"[dump] sysstatemgr.exe::!SysStateMgr ms=0x00000003",
		" waits on ssmsem pc=0x80001004",
		" lr=0x80001108",
		" sp=0x00403f00",
		" s=0x00000007",
		" s=0x80001104",
		"",
		"[EKERN][tick] interleaved line",
		"[dump] efile.exe::Main ms=0x00000005",
		"[dump] ===== end =====",
		"[dump] later.exe::X ms=0x00000002",
	}
	ts := parseDump(lines, tab)
	if len(ts) != 3 {
		t.Fatalf("%d threads: %+v", len(ts), ts)
	}
	s := ts[1]
	if s.Thread != "sysstatemgr.exe::!SysStateMgr" || s.State != "waitsem" || s.WaitsOn != "ssmsem" ||
		s.PC != "Exec::WaitForAnyRequest() [euser.dll] (0x80001004)" || s.LR != "CActiveScheduler::Start() [euser.dll] (0x80001108)" {
		t.Fatalf("%+v", s)
	}
	if s.SP != "0x00403f00" || len(s.Stack) != 2 || s.Stack[0] != "0x00000007" ||
		s.Stack[1] != "CActiveScheduler::Start() [euser.dll] (0x80001104)" {
		t.Fatalf("stack %+v", s)
	}
	if ts[0].State != "ready (or blocked in the nanokernel)" || ts[2].State != "waitmutex" || ts[2].PC != "" {
		t.Fatalf("%+v %+v", ts[0], ts[2])
	}
}
