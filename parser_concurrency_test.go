/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

func TestConcurrency_ParallelRequests(t *testing.T) {
	type Req struct {
		Name string `query:"name" validate:"required"`
	}
	handler := RequestParser(captureHandler[Req])
	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, "/?name=user", nil)
			req.URL.RawQuery = "name=user" + strconv.Itoa(idx)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("request %d: status = %d, want %d", idx, rec.Code, http.StatusOK)
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrency_ChiRouter(t *testing.T) {
	type Req struct {
		ID string `url:"id" validate:"required"`
	}
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			target := "/" + strconv.Itoa(idx)
			_, rec := doChiRequest[Req](t, http.MethodGet, "/{id}", target, captureHandler[Req])
			if rec.Code != http.StatusOK {
				t.Errorf("request %d: status = %d, want %d", idx, rec.Code, http.StatusOK)
			}
		}(i)
	}
	wg.Wait()
}
