// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package h264 is the little H.264 (ITU-T H.264) bitstream work a live view needs: read the parameter sets, rewrite
// a slice's frame_num, and write a P picture whose macroblocks are all skipped (it repeats the reference picture).
// A screen encoder that sends only changed pictures becomes a constant-rate stream this way -- what a browser's
// Media Source Extensions need to play it without stalls. CAVLC streams with frames only (no fields).
package h264

import "errors"

// Params is what this package needs from the SPS and PPS.
type Params struct {
	Log2MaxFrameNum int
	PocType         int
	MbW, MbH        int
	CABAC           bool
	BottomFieldPoc  bool
	Weighted        bool
	DeblockControl  bool
	Redundant       bool
}

// Unescape removes emulation prevention bytes (00 00 03 -> 00 00).
func Unescape(d []byte) []byte {
	o := make([]byte, 0, len(d))
	z := 0
	for _, x := range d {
		if z >= 2 && x == 3 {
			z = 0
			continue
		}
		o = append(o, x)
		if x == 0 {
			z++
		} else {
			z = 0
		}
	}
	return o
}

// Escape inserts emulation prevention bytes.
func Escape(d []byte) []byte {
	o := make([]byte, 0, len(d)+len(d)/64+4)
	z := 0
	for _, x := range d {
		if z >= 2 && x <= 3 {
			o = append(o, 3)
			z = 0
		}
		o = append(o, x)
		if x == 0 {
			z++
		} else {
			z = 0
		}
	}
	return o
}

type reader struct {
	d []byte
	i int
}

var errShort = errors.New("h264: bitstream too short")

func (r *reader) u(n int) (uint32, error) {
	var v uint32
	for k := 0; k < n; k++ {
		if r.i>>3 >= len(r.d) {
			return 0, errShort
		}
		v = v<<1 | uint32(r.d[r.i>>3]>>(7-uint(r.i&7))&1)
		r.i++
	}
	return v, nil
}

func (r *reader) ue() (uint32, error) {
	z := 0
	for {
		b, err := r.u(1)
		if err != nil {
			return 0, err
		}
		if b == 1 {
			break
		}
		if z++; z > 31 {
			return 0, errors.New("h264: bad exp-Golomb code")
		}
	}
	v, err := r.u(z)
	return 1<<z - 1 + v, err
}

func (r *reader) se() (int32, error) {
	k, err := r.ue()
	if k&1 != 0 {
		return int32(k+1) / 2, err
	}
	return -int32(k / 2), err
}

// ParseParams reads an SPS and a PPS NAL unit (with their NAL headers, escaped).
func ParseParams(sps, pps []byte) (Params, error) {
	var p Params
	if len(sps) < 4 || len(pps) < 2 {
		return p, errShort
	}
	profile := sps[1]
	r := &reader{d: Unescape(sps[4:])}
	if _, err := r.ue(); err != nil { // seq_parameter_set_id
		return p, err
	}
	switch profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		return p, errors.New("h264: high profiles are not handled")
	}
	v, err := r.ue()
	if err != nil {
		return p, err
	}
	p.Log2MaxFrameNum = int(v) + 4
	if v, err = r.ue(); err != nil {
		return p, err
	}
	p.PocType = int(v)
	if p.PocType != 2 {
		// type 2 derives the order from frame_num, which SkipP / SetFrameNum keep consistent; 0 and 1 would need the
		// real pictures' order fields rewritten as well
		return p, errors.New("h264: only pic_order_cnt_type 2 is handled")
	}
	if _, err = r.ue(); err != nil { // max_num_ref_frames
		return p, err
	}
	if _, err = r.u(1); err != nil { // gaps_in_frame_num_value_allowed_flag
		return p, err
	}
	w, _ := r.ue()
	h, _ := r.ue()
	fmo, err := r.u(1)
	if err != nil {
		return p, err
	}
	if fmo == 0 {
		return p, errors.New("h264: field coding is not handled")
	}
	p.MbW, p.MbH = int(w)+1, int(h)+1

	r = &reader{d: Unescape(pps[1:])}
	r.ue() // pps id
	r.ue() // sps id
	b, _ := r.u(1)
	p.CABAC = b == 1
	b, _ = r.u(1)
	p.BottomFieldPoc = b == 1
	if g, _ := r.ue(); g != 0 {
		return p, errors.New("h264: slice groups are not handled")
	}
	r.ue() // num_ref_idx_l0_default_active_minus1
	r.ue() // l1
	b, _ = r.u(1)
	p.Weighted = b == 1
	r.u(2) // weighted_bipred_idc
	r.se() // pic_init_qp_minus26
	r.se() // pic_init_qs_minus26
	r.se() // chroma_qp_index_offset
	b, _ = r.u(1)
	p.DeblockControl = b == 1
	r.u(1) // constrained_intra_pred_flag
	b, err = r.u(1)
	p.Redundant = b == 1
	if err != nil {
		return p, err
	}
	if p.CABAC {
		return p, errors.New("h264: CABAC streams are not handled")
	}
	if p.Weighted {
		return p, errors.New("h264: weighted prediction is not handled")
	}
	return p, nil
}

// SetFrameNum rewrites frame_num in a slice NAL unit (header byte + escaped payload); other NAL types come back as is.
func SetFrameNum(nal []byte, p Params, fn uint32) ([]byte, error) {
	if len(nal) < 2 || (nal[0]&0x1F != 1 && nal[0]&0x1F != 5) {
		return nal, nil
	}
	d := Unescape(nal[1:])
	r := &reader{d: d}
	for k := 0; k < 3; k++ { // first_mb_in_slice, slice_type, pic_parameter_set_id
		if _, err := r.ue(); err != nil {
			return nil, err
		}
	}
	at := r.i
	if at+p.Log2MaxFrameNum > len(d)*8 {
		return nil, errShort
	}
	for k := 0; k < p.Log2MaxFrameNum; k++ {
		bit := byte(fn>>(uint(p.Log2MaxFrameNum-1-k))) & 1
		pos := at + k
		m := byte(0x80) >> uint(pos&7)
		d[pos>>3] = d[pos>>3]&^m | bit<<(7-uint(pos&7))
	}
	return append([]byte{nal[0]}, Escape(d)...), nil
}

type writer struct {
	d []byte
	n int
}

func (w *writer) u(n int, v uint32) {
	for k := n - 1; k >= 0; k-- {
		if w.n&7 == 0 {
			w.d = append(w.d, 0)
		}
		w.d[len(w.d)-1] |= byte(v>>uint(k)&1) << (7 - uint(w.n&7))
		w.n++
	}
}

func (w *writer) ue(v uint32) {
	x := v + 1
	z := 0
	for t := x; t > 1; t >>= 1 {
		z++
	}
	w.u(z, 0)
	w.u(z+1, x)
}

func (w *writer) se(v int32) {
	if v > 0 {
		w.ue(uint32(2*v - 1))
	} else {
		w.ue(uint32(-2 * v))
	}
}

// SkipP is a reference P picture (one slice NAL, header byte + escaped payload, no start code) whose macroblocks are
// all P_Skip: it repeats the previous reference picture. frameNum = its frame_num.
func SkipP(p Params, frameNum uint32) []byte {
	w := &writer{}
	w.ue(0)                          // first_mb_in_slice
	w.ue(5)                          // slice_type: P (all slices of the picture)
	w.ue(0)                          // pic_parameter_set_id
	w.u(p.Log2MaxFrameNum, frameNum) // frame_num
	if p.Redundant {
		w.ue(0)
	}
	w.u(1, 0) // num_ref_idx_active_override_flag
	w.u(1, 0) // ref_pic_list_modification_flag_l0
	w.u(1, 0) // dec_ref_pic_marking: adaptive_ref_pic_marking_mode_flag (sliding window)
	w.se(0)   // slice_qp_delta
	if p.DeblockControl {
		w.ue(1) // disable_deblocking_filter_idc: off
	}
	w.ue(uint32(p.MbW * p.MbH))                 // slice_data: mb_skip_run = every macroblock
	w.u(1, 1)                                   // rbsp_stop_one_bit (+ zero bits to the byte)
	return append([]byte{0x41}, Escape(w.d)...) // nal_ref_idc 2, type 1 (non-IDR slice)
}
