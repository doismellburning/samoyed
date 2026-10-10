// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// RequireToken lets a request through to next only if it carries token as an
// "Authorization: Bearer" header.  The comparison takes the same time however
// much of the token a guess gets right.
func RequireToken(token string, next http.Handler) http.Handler {
	var want = []byte(token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got, ok = strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || len(want) == 0 || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="samoyed"`)
			http.Error(w, "unauthorised", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}
