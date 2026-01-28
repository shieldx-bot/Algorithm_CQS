//go:build linux

package memory

import (
	"bytes"
	"encoding/json"
	"sort"
)

type jsonLineLogger interface {
	Print(v ...any)
}

func marshalOrderedJSON(record map[string]interface{}) ([]byte, error) {
	priority := []string{"time", "ts", "window_sec"}

	seen := make(map[string]struct{}, len(record))
	remaining := make([]string, 0, len(record))
	for k := range record {
		seen[k] = struct{}{}
		remaining = append(remaining, k)
	}
	for _, k := range priority {
		if _, ok := seen[k]; ok {
			for i := 0; i < len(remaining); i++ {
				if remaining[i] == k {
					remaining = append(remaining[:i], remaining[i+1:]...)
					i--
				}
			}
		}
	}
	sort.Strings(remaining)

	buf := &bytes.Buffer{}
	buf.WriteByte('{')
	first := true
	writeKV := func(k string, v interface{}) error {
		kb, err := json.Marshal(k)
		if err != nil {
			return err
		}
		vb, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
		return nil
	}

	for _, k := range priority {
		if v, ok := record[k]; ok {
			if err := writeKV(k, v); err != nil {
				return nil, err
			}
		}
	}
	for _, k := range remaining {
		if err := writeKV(k, record[k]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeJSONLine(logger jsonLineLogger, record map[string]interface{}) {
	b, err := marshalOrderedJSON(record)
	if err != nil {
		return
	}
	logger.Print(string(b))
}
