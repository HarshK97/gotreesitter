package main

// A minimal, read-only decoder for the pprof profile.proto wire format
// (https://github.com/google/pprof/blob/main/proto/profile.proto). This tool
// deliberately does not depend on github.com/google/pprof: the root module
// and the cgo_harness module both keep a minimal dependency footprint, and
// attribution measurement infrastructure should not widen it. The decoder
// only reads the handful of fields the attribution mapper needs: sample
// values, sample call stacks (location IDs), locations (function IDs and
// line numbers), functions (name and filename string-table indices), and the
// string table itself. It ignores every other field via generic wire-type
// skipping, so it stays correct across pprof profile.proto's optional fields.

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
)

// pbFrame is one resolved stack frame: a function name and its defining file,
// in the profile's string table.
type pbFrame struct {
	Function string
	File     string
	Line     int64
}

// pbSample is one CPU-profile sample: a leaf-to-root call stack and the
// sampled value (nanoseconds of on-CPU time for a Go CPU profile).
type pbSample struct {
	Stack   []pbFrame // index 0 is the leaf (innermost, currently executing) frame.
	ValueNS int64
}

// pbProfile is the decoded subset of a pprof CPU profile this tool needs.
type pbProfile struct {
	Samples []pbSample
}

type pbLine struct {
	FunctionID uint64
	LineNo     int64
}

type pbLocation struct {
	ID    uint64
	Lines []pbLine
}

type pbFunction struct {
	ID          uint64
	NameIdx     int64
	FilenameIdx int64
}

type pbValueType struct {
	TypeIdx int64
	UnitIdx int64
}

type pbRawSample struct {
	LocationIDs []uint64
	Values      []int64
}

// decodePProfCPUProfile reads a gzip-compressed pprof CPU profile from path
// and resolves it into leaf-to-root frame stacks with their CPU-nanosecond
// value, using the sample_type entry named "cpu".
func decodePProfCPUProfile(path string) (*pbProfile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: gzip: %w", path, err)
	}
	data, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("%s: decompress: %w", path, err)
	}

	dec := &pbReader{buf: data}
	var stringTable []string
	var valueTypes []pbValueType
	var rawSamples []pbRawSample
	locations := map[uint64]pbLocation{}
	functions := map[uint64]pbFunction{}

	for !dec.done() {
		field, wireType, err := dec.tag()
		if err != nil {
			return nil, fmt.Errorf("%s: tag: %w", path, err)
		}
		switch {
		case field == 1 && wireType == 2: // sample_type
			body, err := dec.bytes()
			if err != nil {
				return nil, fmt.Errorf("%s: sample_type: %w", path, err)
			}
			vt, err := decodeValueType(body)
			if err != nil {
				return nil, fmt.Errorf("%s: sample_type: %w", path, err)
			}
			valueTypes = append(valueTypes, vt)
		case field == 2 && wireType == 2: // sample
			body, err := dec.bytes()
			if err != nil {
				return nil, fmt.Errorf("%s: sample: %w", path, err)
			}
			sample, err := decodeRawSample(body)
			if err != nil {
				return nil, fmt.Errorf("%s: sample: %w", path, err)
			}
			rawSamples = append(rawSamples, sample)
		case field == 4 && wireType == 2: // location
			body, err := dec.bytes()
			if err != nil {
				return nil, fmt.Errorf("%s: location: %w", path, err)
			}
			loc, err := decodeLocation(body)
			if err != nil {
				return nil, fmt.Errorf("%s: location: %w", path, err)
			}
			locations[loc.ID] = loc
		case field == 5 && wireType == 2: // function
			body, err := dec.bytes()
			if err != nil {
				return nil, fmt.Errorf("%s: function: %w", path, err)
			}
			fn, err := decodeFunction(body)
			if err != nil {
				return nil, fmt.Errorf("%s: function: %w", path, err)
			}
			functions[fn.ID] = fn
		case field == 6 && wireType == 2: // string_table entry
			body, err := dec.bytes()
			if err != nil {
				return nil, fmt.Errorf("%s: string_table: %w", path, err)
			}
			stringTable = append(stringTable, string(body))
		default:
			if err := dec.skip(wireType); err != nil {
				return nil, fmt.Errorf("%s: skip field %d: %w", path, field, err)
			}
		}
	}

	str := func(idx int64) string {
		if idx < 0 || int(idx) >= len(stringTable) {
			return ""
		}
		return stringTable[idx]
	}

	valueIndex := -1
	for i, vt := range valueTypes {
		if str(vt.TypeIdx) == "cpu" {
			valueIndex = i
			break
		}
	}
	if valueIndex == -1 {
		if len(valueTypes) >= 2 {
			valueIndex = 1 // runtime/pprof CPU profiles: [0]=samples/count, [1]=cpu/nanoseconds.
		} else {
			return nil, fmt.Errorf("%s: no \"cpu\" sample_type found (have %d)", path, len(valueTypes))
		}
	}

	resolveFrame := func(functionID uint64, lineNo int64) pbFrame {
		fn, ok := functions[functionID]
		if !ok {
			return pbFrame{Function: fmt.Sprintf("<unknown function %d>", functionID), Line: lineNo}
		}
		return pbFrame{Function: str(fn.NameIdx), File: str(fn.FilenameIdx), Line: lineNo}
	}

	profile := &pbProfile{}
	for _, raw := range rawSamples {
		if valueIndex >= len(raw.Values) {
			return nil, fmt.Errorf("%s: sample has %d values, want index %d", path, len(raw.Values), valueIndex)
		}
		sample := pbSample{ValueNS: raw.Values[valueIndex]}
		for _, locID := range raw.LocationIDs {
			loc, ok := locations[locID]
			if !ok {
				sample.Stack = append(sample.Stack, pbFrame{Function: fmt.Sprintf("<unknown location %d>", locID)})
				continue
			}
			// Line[0] is the innermost inlined frame; keep that order (leaf-first).
			for _, line := range loc.Lines {
				sample.Stack = append(sample.Stack, resolveFrame(line.FunctionID, line.LineNo))
			}
			if len(loc.Lines) == 0 {
				sample.Stack = append(sample.Stack, pbFrame{Function: fmt.Sprintf("<no line info, location %d>", locID)})
			}
		}
		profile.Samples = append(profile.Samples, sample)
	}
	return profile, nil
}

func decodeValueType(body []byte) (pbValueType, error) {
	dec := &pbReader{buf: body}
	var vt pbValueType
	for !dec.done() {
		field, wireType, err := dec.tag()
		if err != nil {
			return vt, err
		}
		switch {
		case field == 1 && wireType == 0:
			v, err := dec.varint()
			if err != nil {
				return vt, err
			}
			vt.TypeIdx = int64(v)
		case field == 2 && wireType == 0:
			v, err := dec.varint()
			if err != nil {
				return vt, err
			}
			vt.UnitIdx = int64(v)
		default:
			if err := dec.skip(wireType); err != nil {
				return vt, err
			}
		}
	}
	return vt, nil
}

func decodeRawSample(body []byte) (pbRawSample, error) {
	dec := &pbReader{buf: body}
	var sample pbRawSample
	for !dec.done() {
		field, wireType, err := dec.tag()
		if err != nil {
			return sample, err
		}
		switch {
		case field == 1: // location_id: repeated uint64, packed or unpacked.
			ids, err := decodeVarintList(dec, wireType)
			if err != nil {
				return sample, err
			}
			for _, id := range ids {
				sample.LocationIDs = append(sample.LocationIDs, id)
			}
		case field == 2: // value: repeated int64, packed or unpacked.
			values, err := decodeVarintList(dec, wireType)
			if err != nil {
				return sample, err
			}
			for _, v := range values {
				sample.Values = append(sample.Values, int64(v))
			}
		default:
			if err := dec.skip(wireType); err != nil {
				return sample, err
			}
		}
	}
	return sample, nil
}

func decodeLocation(body []byte) (pbLocation, error) {
	dec := &pbReader{buf: body}
	var loc pbLocation
	for !dec.done() {
		field, wireType, err := dec.tag()
		if err != nil {
			return loc, err
		}
		switch {
		case field == 1 && wireType == 0:
			v, err := dec.varint()
			if err != nil {
				return loc, err
			}
			loc.ID = v
		case field == 4 && wireType == 2: // line (repeated Line message)
			lineBody, err := dec.bytes()
			if err != nil {
				return loc, err
			}
			line, err := decodeLine(lineBody)
			if err != nil {
				return loc, err
			}
			loc.Lines = append(loc.Lines, line)
		default:
			if err := dec.skip(wireType); err != nil {
				return loc, err
			}
		}
	}
	return loc, nil
}

func decodeLine(body []byte) (pbLine, error) {
	dec := &pbReader{buf: body}
	var line pbLine
	for !dec.done() {
		field, wireType, err := dec.tag()
		if err != nil {
			return line, err
		}
		switch {
		case field == 1 && wireType == 0:
			v, err := dec.varint()
			if err != nil {
				return line, err
			}
			line.FunctionID = v
		case field == 2 && wireType == 0:
			v, err := dec.varint()
			if err != nil {
				return line, err
			}
			line.LineNo = int64(v)
		default:
			if err := dec.skip(wireType); err != nil {
				return line, err
			}
		}
	}
	return line, nil
}

func decodeFunction(body []byte) (pbFunction, error) {
	dec := &pbReader{buf: body}
	var fn pbFunction
	for !dec.done() {
		field, wireType, err := dec.tag()
		if err != nil {
			return fn, err
		}
		switch {
		case field == 1 && wireType == 0:
			v, err := dec.varint()
			if err != nil {
				return fn, err
			}
			fn.ID = v
		case field == 2 && wireType == 0:
			v, err := dec.varint()
			if err != nil {
				return fn, err
			}
			fn.NameIdx = int64(v)
		case field == 4 && wireType == 0:
			v, err := dec.varint()
			if err != nil {
				return fn, err
			}
			fn.FilenameIdx = int64(v)
		default:
			if err := dec.skip(wireType); err != nil {
				return fn, err
			}
		}
	}
	return fn, nil
}

// decodeVarintList reads a repeated varint field's value(s) starting after the
// tag has already been consumed. wireType 2 means packed (a length-delimited
// blob of back-to-back varints); wireType 0 means one unpacked varint.
func decodeVarintList(dec *pbReader, wireType int) ([]uint64, error) {
	switch wireType {
	case 2:
		body, err := dec.bytes()
		if err != nil {
			return nil, err
		}
		sub := &pbReader{buf: body}
		var out []uint64
		for !sub.done() {
			v, err := sub.varint()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case 0:
		v, err := dec.varint()
		if err != nil {
			return nil, err
		}
		return []uint64{v}, nil
	default:
		return nil, fmt.Errorf("unsupported wire type %d for varint list", wireType)
	}
}

// pbReader is a minimal forward-only protobuf wire-format cursor.
type pbReader struct {
	buf []byte
	pos int
}

func (r *pbReader) done() bool { return r.pos >= len(r.buf) }

func (r *pbReader) varint() (uint64, error) {
	var x uint64
	var shift uint
	for {
		if r.pos >= len(r.buf) {
			return 0, io.ErrUnexpectedEOF
		}
		b := r.buf[r.pos]
		r.pos++
		if shift >= 64 {
			return 0, errors.New("pbreader: varint too long")
		}
		x |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return x, nil
		}
		shift += 7
	}
}

func (r *pbReader) tag() (field int, wireType int, err error) {
	v, err := r.varint()
	if err != nil {
		return 0, 0, err
	}
	return int(v >> 3), int(v & 0x7), nil
}

func (r *pbReader) bytes() ([]byte, error) {
	n, err := r.varint()
	if err != nil {
		return nil, err
	}
	if n > uint64(len(r.buf)-r.pos) {
		return nil, io.ErrUnexpectedEOF
	}
	out := r.buf[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return out, nil
}

func (r *pbReader) skip(wireType int) error {
	switch wireType {
	case 0:
		_, err := r.varint()
		return err
	case 1:
		if len(r.buf)-r.pos < 8 {
			return io.ErrUnexpectedEOF
		}
		r.pos += 8
		return nil
	case 2:
		_, err := r.bytes()
		return err
	case 5:
		if len(r.buf)-r.pos < 4 {
			return io.ErrUnexpectedEOF
		}
		r.pos += 4
		return nil
	default:
		return fmt.Errorf("pbreader: unsupported wire type %d", wireType)
	}
}
