// Copyright (c) Nokia Corporation and/or its subsidiary(-ies) (ThemeInstaller); Copyright The Phorepoc Authors (the Go port)
// SPDX-License-Identifier: EPL-1.0

package odt

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The converter's output depends on a few Java library behaviours; they are reproduced here.

// javaStringHash is String.hashCode.
func javaStringHash(s string) int32 {
	var h int32
	for _, u := range utf16.Encode([]rune(s)) {
		h = 31*h + int32(u)
	}
	return h
}

// javaHashMapOrder returns the iteration order of a java.util.HashMap (Java 8 and later) that had the given
// distinct keys put into it in the given order. ThemeInstaller walks a rule's style map, so the order of the
// properties in the output is the map's.
func javaHashMapOrder(keys []string) []string {
	capacity := 16
	buckets := make([][]string, capacity)
	size := 0
	idx := func(k string, n int) int {
		h := uint32(javaStringHash(k))
		h ^= h >> 16
		return int(h) & (n - 1)
	}
	for _, k := range keys {
		i := idx(k, capacity)
		buckets[i] = append(buckets[i], k)
		size++
		if size > capacity*3/4 {
			nb := make([][]string, capacity*2)
			for _, b := range buckets {
				for _, key := range b {
					j := idx(key, capacity*2)
					nb[j] = append(nb[j], key)
				}
			}
			buckets = nb
			capacity *= 2
		}
	}
	var out []string
	for _, b := range buckets {
		out = append(out, b...)
	}
	return out
}

// javaFloatString is Float.toString for a float32.
func javaFloatString(f float32) string {
	if f != f {
		return "NaN"
	}
	a := f
	if a < 0 {
		a = -a
	}
	if a == 0 {
		if 1/f < 0 {
			return "-0.0"
		}
		return "0.0"
	}
	if a >= 1e-3 && a < 1e7 {
		s := strconv.FormatFloat(float64(f), 'f', -1, 32)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	s := strconv.FormatFloat(float64(f), 'e', -1, 32) // d.ddde+XX
	mant, exp, _ := strings.Cut(s, "e")
	if !strings.Contains(mant, ".") {
		mant += ".0"
	}
	e, _ := strconv.Atoi(exp)
	return mant + "E" + strconv.Itoa(e)
}

// latin1Bytes is String.getBytes() under -Dfile.encoding=ISO8859_1: characters above U+00FF become '?'.
func latin1Bytes(s string) []byte {
	var out []byte
	for _, u := range utf16.Encode([]rune(s)) {
		if u > 0xff {
			out = append(out, '?')
		} else {
			out = append(out, byte(u))
		}
	}
	return out
}

// utf16Len is String.length.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// scsuCompress is com.ibm.icu.text.UnicodeCompressor.compress for text that stays in the initial (Basic Latin)
// window: printable ASCII and TAB, LF, CR are literal, other control characters are quoted with SQ0.
func scsuCompress(s string) ([]byte, error) {
	var out []byte
	for _, r := range s {
		switch {
		case r == 0x09 || r == 0x0a || r == 0x0d || (r >= 0x20 && r <= 0x7f):
			out = append(out, byte(r))
		case r < 0x20:
			out = append(out, 0x01, byte(r)) // SQ0 + the control character
		default:
			return nil, fmt.Errorf("odt: string %q has non-ASCII text, which the SCSU writer does not cover", s)
		}
	}
	return out, nil
}
