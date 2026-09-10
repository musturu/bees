package endpoint

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// TaggedBinder populates an input struct using json, query, header, and path tags.
// - JSON body is decoded into the struct using encoding/json.
// - Fields can override or extend values using `query:"name"`, `header:"X-Id"`, or `path:"id"` tags.
func TaggedBinder[T any](r *http.Request, in *T) error {
	if r == nil || in == nil {
		return fmt.Errorf("endpoint: request or input nil")
	}

	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return fmt.Errorf("endpoint: read body: %w", err)
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, in); err != nil {
				return fmt.Errorf("endpoint: decode body: %w", err)
			}
		}
	}

	v := reflect.ValueOf(in)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("endpoint: input must be a non-nil pointer")
	}
	v = v.Elem()
	t := v.Type()

	query := r.URL.Query()
	headers := r.Header

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)
		if !fv.CanSet() {
			continue
		}

		if tag, ok := field.Tag.Lookup("query"); ok {
			key := tag
			if key == "" {
				key = strings.ToLower(field.Name)
			}
			if vals, ok := query[key]; ok && len(vals) > 0 {
				if err := setScalar(fv, vals[0]); err != nil {
					return fmt.Errorf("endpoint: query %s: %w", key, err)
				}
			}
		}

		if tag, ok := field.Tag.Lookup("header"); ok {
			name := tag
			if name == "" {
				name = field.Name
			}
			name = http.CanonicalHeaderKey(name)
			if value := headers.Get(name); value != "" {
				if err := setScalar(fv, value); err != nil {
					return fmt.Errorf("endpoint: header %s: %w", name, err)
				}
			}
		}

		if tag, ok := field.Tag.Lookup("path"); ok {
			key := tag
			if key == "" {
				key = field.Name
			}
			pv := r.PathValue(key)
			if pv != "" {
				if err := setScalar(fv, pv); err != nil {
					return fmt.Errorf("endpoint: path %s: %w", key, err)
				}
			}
		}
	}

	return nil
}

func setScalar(fv reflect.Value, raw string) error {
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		fv = fv.Elem()
	}

	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v, err := strconv.ParseUint(raw, 10, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetUint(v)
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(raw, fv.Type().Bits())
		if err != nil {
			return err
		}
		fv.SetFloat(v)
	default:
		return fmt.Errorf("endpoint: unsupported field type %s", fv.Kind())
	}
	return nil
}
