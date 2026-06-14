/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package http

import (
	"bufio"
	"io"
	"reflect"
	"runtime"

	"github.com/thanhminhmr/go-exception"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func funcObject(v any) exception.StackFrame {
	if v == nil {
		return exception.StackFrame{
			Function: "<nil>",
			File:     "",
			Line:     0,
		}
	}
	r := reflect.ValueOf(v)
	for r.Kind() == reflect.Ptr || r.Kind() == reflect.Interface {
		r = r.Elem()
	}
	switch r.Kind() {
	case reflect.Func:
		f := runtime.FuncForPC(r.Pointer())
		if f == nil {
			return exception.StackFrame{
				Function: "<unknown>",
				File:     "",
				Line:     0,
			}
		}
		file, line := f.FileLine(f.Entry())
		return exception.StackFrame{
			Function: f.Name(),
			File:     file,
			Line:     line,
		}
	default:
		return exception.StackFrame{
			Function: "<invalid>",
			File:     "",
			Line:     0,
		}
	}
}

func funcObjects[S ~[]E, E any](values S) exception.StackFrames {
	frames := make(exception.StackFrames, 0, len(values))
	for _, value := range values {
		frames = append(frames, funcObject(value))
	}
	return frames
}

var unicodeUTF16LE = unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
var unicodeUTF16BE = unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)

func charsetReader(reader io.Reader, contentTypeParams map[string]string) (io.Reader, error) {
	// check if it is officially have a charset
	if contentCharset, exists := contentTypeParams["charset"]; exists {
		if contentEncoding, _ := charset.Lookup(contentCharset); contentEncoding != nil {
			if contentEncoding != encoding.Nop {
				return transform.NewReader(reader, contentEncoding.NewDecoder()), nil
			}
			return reader, nil
		}
		return nil, exception.String("Charset: Invalid content charset").SetExtra("charset", contentCharset)
	}
	// peek for BOM bytes
	buffered := bufio.NewReader(reader)
	peek, _ := buffered.Peek(3)
	switch {
	case len(peek) >= 3:
		// UTF-8 BOM
		if peek[0] == 0xEF && peek[1] == 0xBB && peek[2] == 0xBF {
			_, _ = buffered.Discard(3)
			return buffered, nil
		}
		fallthrough
	case len(peek) >= 2:
		// UTF-16LE BOM
		if peek[0] == 0xFF && peek[1] == 0xFE {
			_, _ = buffered.Discard(2)
			return transform.NewReader(buffered, unicodeUTF16LE.NewDecoder()), nil
		}
		// UTF-16BE BOM
		if peek[0] == 0xFE && peek[1] == 0xFF {
			_, _ = buffered.Discard(2)
			return transform.NewReader(buffered, unicodeUTF16BE.NewDecoder()), nil
		}
	}
	return buffered, nil
}
