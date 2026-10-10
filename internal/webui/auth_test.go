// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequireToken(t *testing.T) {
	var h = RequireToken("s3cret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for header, want := range map[string]int{
		"":              http.StatusUnauthorized,
		"Bearer":        http.StatusUnauthorized,
		"Bearer wrong":  http.StatusUnauthorized,
		"Basic s3cret":  http.StatusUnauthorized,
		"Bearer s3cret": http.StatusNoContent,
	} {
		var r = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/admin/x", nil)
		if header != "" {
			r.Header.Set("Authorization", header)
		}

		var w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Equal(t, want, w.Code, header)
	}
}

func TestRequireTokenEmptyTokenRefusesAll(t *testing.T) {
	var h = RequireToken("", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	var r = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
	r.Header.Set("Authorization", "Bearer ")

	var w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandlerServesExtraRoutes(t *testing.T) {
	var h = Handler(NewHub(), Route{Pattern: "GET /api/extra", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, map[string]int{"answer": 42})
	})})

	var w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/extra", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"answer": 42}`, w.Body.String())
	assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"), "extra routes get the same headers")
}
