/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/thanhminhmr/go-common/common"
)

// requestTags is immutable compiled binding metadata for one request struct.
// flags records which request sources are present; field-index paths identify
// whole-source fields; bodyContentTypes is the allowlist for a raw body tag.
// Compiled values are cached and reused concurrently after construction.
type requestTags struct {
	flags               uint
	headerFieldIndex    []int
	cookieFieldIndex    []int
	queryFieldIndex     []int
	urlFieldIndex       []int
	formFieldIndex      []int
	jsonFieldIndex      []int
	multipartFieldIndex []int
	bodyFieldIndex      []int
	bodyContentTypes    []string
}

const (
	tagHeader uint = 1 << iota
	tagCookie
	tagQuery
	tagUrl
	tagForm
	tagJson
	tagMultipart
	tagBody
)

var typeForHeader = reflect.TypeFor[http.Header]()
var typeForKeyValues = reflect.TypeFor[KeyValues]()
var typeForKeyValue = reflect.TypeFor[KeyValue]()
var typeForMultipartReader = reflect.TypeFor[*multipart.Reader]()
var typeForReadCloser = reflect.TypeFor[io.ReadCloser]()

// The request-tag cache is process-wide. globalTagsMutex protects both cache
// lookup and compilation so a request type is compiled at most once at a time.
var globalTags = map[reflect.Type]requestTags{}
var globalTagsMutex sync.Mutex

// createTags returns cached binding metadata for requestType or compiles it on
// first use. Request must be a struct. Default tags and binding-tag structure
// are checked during compilation; invalid request definitions panic.
func createTags(requestType reflect.Type) requestTags {
	if requestType.Kind() != reflect.Struct {
		panic("BUG: parsed request must be a struct")
	}
	globalTagsMutex.Lock()
	defer globalTagsMutex.Unlock()
	tags, exists := globalTags[requestType]
	if exists {
		return tags
	}
	if err := common.ApplyDefaults(reflect.New(requestType).Interface()); err != nil {
		panic(err)
	}
	tags.checkRecursively(requestType, nil)
	if len(tags.bodyContentTypes) > 0 {
		if tags.flags&tagForm != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsForm) {
			panic("BUG: `form` tag field is not allowed when `body` tag contains " + contentTypeIsForm)
		} else if tags.flags&tagJson != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsJson) {
			panic("BUG: `json` tag field is not allowed when `body` tag contains " + contentTypeIsJson)
		} else if tags.flags&tagMultipart != 0 && slices.Contains(tags.bodyContentTypes, contentTypeIsMultipart) {
			panic("BUG: `multipart` tag field is not allowed when `body` tag contains " + contentTypeIsMultipart)
		}
	}
	globalTags[requestType] = tags
	return tags
}

// checkRecursively scans exported fields in requestType and recursively scans
// anonymous embedded structs. It validates whole-source tag types and
// exclusivity rules while recording field-index paths. Unexported fields are
// ignored; anonymous fields must be structs.
func (tags *requestTags) checkRecursively(requestType reflect.Type, fieldIndex []int) {
	for index := range requestType.NumField() {
		field := requestType.Field(index)
		if field.PkgPath != "" {
			continue
		}
		if field.Anonymous {
			if field.Type.Kind() != reflect.Struct {
				panic("BUG: anonymous field must be a struct")
			}
			tags.checkRecursively(field.Type, append(fieldIndex, field.Index...))
			continue
		}
		if value, exists := field.Tag.Lookup("header"); exists {
			if value != "" {
				if tags.headerFieldIndex != nil {
					panic("BUG: multiple `header` tag fields are not allowed when empty `header` tag is present")
				}
			} else {
				if tags.flags&tagHeader != 0 {
					panic("BUG: multiple `header` tag fields are not allowed when empty `header` tag is present")
				}
				if field.Type != typeForHeader {
					panic("BUG: empty `header` tag field must be a `http.Header`")
				}
				tags.headerFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagHeader
		}
		if value, exists := field.Tag.Lookup("cookie"); exists {
			if value != "" {
				if tags.cookieFieldIndex != nil {
					panic("BUG: multiple `cookie` tag fields are not allowed when empty `cookie` tag is present")
				}
			} else {
				if tags.flags&tagCookie != 0 {
					panic("BUG: multiple `cookie` tag fields are not allowed when empty `cookie` tag is present")
				}
				if field.Type != typeForKeyValues {
					panic("BUG: empty `cookie` tag field must be a `httpserver.KeyValues`")
				}
				tags.cookieFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagCookie
		}
		if value, exists := field.Tag.Lookup("query"); exists {
			if value != "" {
				if tags.queryFieldIndex != nil {
					panic("BUG: multiple `query` tag fields are not allowed when empty `query` tag is present")
				}
			} else {
				if tags.flags&tagQuery != 0 {
					panic("BUG: multiple `query` tag fields are not allowed when empty `query` tag is present")
				}
				if field.Type != typeForKeyValues {
					panic("BUG: empty `query` tag field must be a `httpserver.KeyValues`")
				}
				tags.queryFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagQuery
		}
		if value, exists := field.Tag.Lookup("url"); exists {
			if value != "" {
				if tags.urlFieldIndex != nil {
					panic("BUG: multiple `url` tag fields are not allowed when empty `url` tag is present")
				}
			} else {
				if tags.flags&tagUrl != 0 {
					panic("BUG: multiple `url` tag fields are not allowed when empty `url` tag is present")
				}
				if field.Type != typeForKeyValue {
					panic("BUG: empty `url` tag field must be a `httpserver.KeyValue`")
				}
				tags.urlFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagUrl
		}
		if value, exists := field.Tag.Lookup("form"); exists {
			if value != "" {
				if tags.formFieldIndex != nil {
					panic("BUG: multiple `form` tag fields are not allowed when empty `form` tag is present")
				}
			} else {
				if tags.flags&tagForm != 0 {
					panic("BUG: multiple `form` tag fields are not allowed when empty `form` tag is present")
				}
				if field.Type != typeForKeyValues {
					panic("BUG: empty `form` tag field must be a `httpserver.KeyValues`")
				}
				tags.formFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagForm
		}
		if value, exists := field.Tag.Lookup("json"); exists {
			if value != "" {
				if tags.jsonFieldIndex != nil {
					panic("BUG: multiple `json` tag fields are not allowed when empty `json` tag is present")
				}
			} else {
				if tags.flags&tagJson != 0 {
					panic("BUG: multiple `json` tag fields are not allowed when empty `json` tag is present")
				}
				tags.jsonFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			}
			tags.flags = tags.flags | tagJson
		}
		if value, exists := field.Tag.Lookup("multipart"); exists {
			if value != "" {
				panic("BUG: `multipart` tag value must be empty")
			}
			if tags.flags&tagMultipart != 0 {
				panic("BUG: multiple `multipart` tag fields are not allowed")
			}
			if field.Type != typeForMultipartReader {
				panic("BUG: `multipart` tag field must be a `*multipart.Reader`")
			}
			tags.flags = tags.flags | tagMultipart
			tags.multipartFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
		}
		if value, exists := field.Tag.Lookup("body"); exists {
			if tags.flags&tagBody != 0 {
				panic("BUG: multiple `body` tag fields are not allowed")
			}
			if field.Type != typeForReadCloser {
				panic("BUG: `body` tag field must be a `io.ReadCloser`")
			}
			tags.flags = tags.flags | tagBody
			tags.bodyFieldIndex = append(append([]int(nil), fieldIndex...), field.Index...)
			if value != "" {
				tags.bodyContentTypes = strings.Fields(value)
			}
		}
	}
}
