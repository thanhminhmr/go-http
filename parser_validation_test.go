/*
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */

package httpserver

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for the validate tag using go-playground/validator.

func TestValidation_Required_Header(t *testing.T) {
	type Req struct {
		Name string `header:"X-Name" validate:"required"`
	}
	t.Run("present", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withHeader("X-Name", "alice"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "alice", captured.request.Name, "Name")
	})
	t.Run("missing_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Empty(t, rec.Body.String())
	})
}

func TestValidation_Required_Query(t *testing.T) {
	type Req struct {
		Age int `query:"age" validate:"required"`
	}
	t.Run("present", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/?age=30", withQuery("age=30"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 30, captured.request.Age, "Age")
	})
	t.Run("missing_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestValidation_Min_Max(t *testing.T) {
	type Req struct {
		Age int `query:"age" validate:"min=18,max=120"`
	}
	t.Run("in_range", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("age=30"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 30, captured.request.Age, "Age")
	})
	t.Run("below_min_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("age=10"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("above_max_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("age=200"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("at_min_boundary", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("age=18"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 18, captured.request.Age, "Age")
	})
	t.Run("at_max_boundary", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("age=120"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, 120, captured.request.Age, "Age")
	})
}

func TestValidation_Email(t *testing.T) {
	type Req struct {
		Email string `query:"email" validate:"required,email"`
	}
	t.Run("valid_email", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("email=user@example.com"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "user@example.com", captured.request.Email, "Email")
	})
	t.Run("invalid_email_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("email=not-an-email"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestValidation_OneOf(t *testing.T) {
	type Req struct {
		Role string `query:"role" validate:"required,oneof=admin user guest"`
	}
	for _, tc := range []struct {
		name  string
		role  string
		valid bool
	}{
		{"admin", "admin", true},
		{"user", "user", true},
		{"guest", "guest", true},
		{"invalid", "superadmin", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("role="+tc.role))
			if tc.valid {
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, tc.role, captured.request.Role, "Role")
			} else {
				assert.Equal(t, http.StatusBadRequest, rec.Code)
			}
		})
	}
}

func TestValidation_String_Length(t *testing.T) {
	type Req struct {
		Password string `json:"password" validate:"required,min=8,max=64"`
	}
	t.Run("valid_length", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withJSONBody(map[string]string{"password": "securepass123"}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "securepass123", captured.request.Password, "Password")
	})
	t.Run("too_short_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withJSONBody(map[string]string{"password": "short"}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("too_long_returns_400", func(t *testing.T) {
		long := make([]byte, 65)
		for i := range long {
			long[i] = 'x'
		}
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withJSONBody(map[string]string{"password": string(long)}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestValidation_Nested_Struct(t *testing.T) {
	type Address struct {
		Street string `json:"street" validate:"required"`
		City   string `json:"city" validate:"required"`
	}
	type Req struct {
		Name    string  `json:"name" validate:"required"`
		Address Address `json:"address" validate:"required"`
	}
	t.Run("valid_nested", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withJSONBody(map[string]any{
			"name": "alice",
			"address": map[string]string{
				"street": "123 Main St",
				"city":   "Springfield",
			},
		}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "alice", captured.request.Name, "Name")
		assert.Equal(t, "123 Main St", captured.request.Address.Street, "Street")
		assert.Equal(t, "Springfield", captured.request.Address.City, "City")
	})
	t.Run("nested_missing_field_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withJSONBody(map[string]any{
			"name": "alice",
			"address": map[string]string{
				"street": "123 Main St",
			},
		}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("nested_whole_missing_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withJSONBody(map[string]any{
			"name": "alice",
		}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestValidation_Slice_Elements(t *testing.T) {
	type Req struct {
		Tags []string `query:"tag" validate:"required,min=1,dive,required"`
	}
	t.Run("valid_slice", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("tag=go&tag=http"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, []string{"go", "http"}, captured.request.Tags, "Tags")
	})
	t.Run("empty_slice_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestValidation_Url(t *testing.T) {
	type Req struct {
		Website string `query:"website" validate:"required,url"`
	}
	t.Run("valid_url", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("website=https://example.com"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "https://example.com", captured.request.Website, "Website")
	})
	t.Run("invalid_url_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withQuery("website=not a url"))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestValidation_Multiple_Fields(t *testing.T) {
	type Req struct {
		Name  string `json:"name" validate:"required,min=2"`
		Email string `json:"email" validate:"required,email"`
		Age   int    `query:"age" validate:"required,min=18"`
	}
	t.Run("all_valid", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/?age=25", withJSONBody(map[string]any{
			"name":  "alice",
			"email": "alice@example.com",
		}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "alice", captured.request.Name, "Name")
		assert.Equal(t, "alice@example.com", captured.request.Email, "Email")
		assert.Equal(t, 25, captured.request.Age, "Age")
	})
	t.Run("one_invalid_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/?age=25", withJSONBody(map[string]any{
			"name":  "a",
			"email": "alice@example.com",
		}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("all_invalid_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/?age=5", withJSONBody(map[string]any{
			"name":  "",
			"email": "not-email",
		}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestValidation_Default_Still_Validated(t *testing.T) {
	type Req struct {
		Name string `header:"X-Name" default:"guest" validate:"required"`
	}
	t.Run("default_satisfies_required", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "guest", captured.request.Name, "Name")
	})
	t.Run("provided_overrides_default", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/", withHeader("X-Name", "alice"))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "alice", captured.request.Name, "Name")
	})
}

func TestValidation_Optional_No_Required(t *testing.T) {
	type Req struct {
		Nickname string `header:"X-Nickname"`
	}
	t.Run("missing_optional_ok", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodGet, "/")
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestValidation_Form_Body(t *testing.T) {
	type Req struct {
		Username string `form:"username" validate:"required,min=3"`
	}
	t.Run("valid", func(t *testing.T) {
		captured, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withFormBody(url.Values{"username": {"alice"}}))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "alice", captured.request.Username, "Username")
	})
	t.Run("too_short_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withFormBody(url.Values{"username": {"ab"}}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("missing_returns_400", func(t *testing.T) {
		_, rec := doRequest[Req](t, captureHandler[Req], http.MethodPost, "/", withFormBody(url.Values{}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
